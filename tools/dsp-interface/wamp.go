// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

package main

// The WAMP surface of the replacement: seven procedures recovered from the donor
// registers on the Bonefish router, the one topic it subscribes to, and the
// version publication.
//
// One behaviour of the donor is deliberately not reproduced. The donor's
// EVENT_DSP_BOOTUP handler calls com.harman.vui.mutedaccontrol and
// com.harman.vui.muteampcontrol, which transiently unmutes the DAC and the
// amplifier. This service never calls those procedures. It reports the boot
// event and leaves every mute decision to the service that owns the mute
// policy.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

const (
	wampHello       = 1
	wampWelcome     = 2
	wampError       = 8
	wampPublish     = 16
	wampSubscribe   = 32
	wampSubscribed  = 33
	wampEvent       = 36
	wampCall        = 48
	wampResult      = 50
	wampRegister    = 64
	wampRegistered  = 65
	wampInvocation  = 68
	wampYield       = 70
	wampMaxFrameLen = 0xffffff

	wampReconnectDelay = 5 * time.Second
	dspCommandTimeout  = 3 * time.Second

	// maxDeferredSetupMessages bounds what one setup step will queue before
	// treating the router as unresponsive.
	maxDeferredSetupMessages = 256
)

const (
	versionTopic = "com.harman.dsp.version"
	stateTopic   = "com.harman.stateChanged"
	sessionTopic = "com.reinvoke.dsp.session"
)

// forbiddenProcedures are the physical mute gates. This service must never
// call them, so the connection refuses them outright.
var forbiddenProcedures = []string{
	"com.harman.vui.mutedaccontrol",
	"com.harman.vui.muteampcontrol",
}

var errDSPLinkFailure = errors.New("DSP link failure")

type procedureSpec struct {
	Name string

	// ID and Opcode are the message id and first payload byte of the frame
	// this procedure emits.
	ID     uint16
	Opcode byte

	// Arguments is how many byte arguments the procedure takes.
	Arguments int
}

// procedures are the eight registrations recovered from the donor's msgwrite
// call sites and confirmed in the captured router log.
var procedures = []procedureSpec{
	{Name: "com.harman.dsp.micTestSingle", ID: messageIDTest, Opcode: 0x00, Arguments: 1},
	{Name: "com.harman.dsp.micTestPair", ID: messageIDTest, Opcode: 0x01, Arguments: 1},
	{Name: "com.harman.dsp.micTestNormal", ID: messageIDTest, Opcode: 0x02},
	{Name: "com.harman.test.dspBypassMode", ID: messageIDTest, Opcode: 0x03, Arguments: 1},
	{Name: "com.harman.dsp.volumeSet", ID: messageIDControl, Opcode: 0x04, Arguments: 1},
	{Name: "com.harman.dsp.getVer", ID: messageIDControl, Opcode: 0x08},
	{Name: "com.harman.dsp.dumpDspMemory", ID: messageIDControl, Opcode: 0x0c, Arguments: 2},
}

var micMuteSpec = procedureSpec{
	Name:      "DSP microphone control",
	ID:        messageIDControl,
	Opcode:    0x09,
	Arguments: 1,
}

// stateChangedSpec is a subscription in the donor, not a registration.
var stateChangedSpec = procedureSpec{
	Name:      stateTopic,
	ID:        messageIDControl,
	Opcode:    0x0b,
	Arguments: 1,
}

// mutePolicy describes how a DSP boot event reaches the owner of the physical
// mute gates. The donor unmuted them itself; this service only reports.
type mutePolicy struct {
	// NotifyTopic is published when the DSP reports EVENT_DSP_BOOTUP.
	NotifyTopic string

	// PolicyProcedure is an optional procedure of the mute policy owner, which
	// remains free to keep the outputs muted.
	PolicyProcedure string

	// BootStatePath records successful DSP boot in volatile runtime state.
	BootStatePath string
}

func (policy mutePolicy) validate() error {
	for _, forbidden := range forbiddenProcedures {
		if policy.PolicyProcedure == forbidden {
			return fmt.Errorf(
				"mute policy procedure %s is an amplifier or DAC gate; "+
					"this service must not request unmute",
				forbidden,
			)
		}
	}
	return nil
}

type wampService struct {
	address string
	realm   string
	link    *link
	policy  mutePolicy

	// safeOnly answers every DSP command with an error while still registering
	// the recovered surface. Device responses remain unverified.
	safeOnly bool

	// idle is how long the message loop sleeps when it moved no traffic.
	idle           time.Duration
	commandTimeout time.Duration
	device         <-chan frame
	ready          <-chan struct{}
	bootEvents     chan<- struct{}
	micControlMu   sync.Mutex

	logf func(string, ...interface{})
}

type wampConnection struct {
	connection net.Conn
	writeMu    sync.Mutex
	nextID     uint64
}

func (service *wampService) log(format string, args ...interface{}) {
	if service.logf != nil {
		service.logf(format, args...)
	}
}

func (service *wampService) run(ctx context.Context) error {
	if err := service.policy.validate(); err != nil {
		return err
	}
	connection, err := net.DialTimeout("tcp", service.address, 5*time.Second)
	if err != nil {
		return fmt.Errorf("connect WAMP router: %w", err)
	}
	defer connection.Close()
	setupDone := make(chan struct{})
	defer close(setupDone)
	go func() {
		select {
		case <-ctx.Done():
			_ = connection.Close()
		case <-setupDone:
		}
	}()
	client := &wampConnection{connection: connection, nextID: 1}
	err = service.serve(ctx, client)
	if ctx.Err() != nil {
		return nil
	}
	return err
}

// serve runs the registered surface over an established connection.
func (service *wampService) serve(
	ctx context.Context,
	client *wampConnection,
) error {
	if err := service.policy.validate(); err != nil {
		return err
	}
	if err := client.negotiate(service.realm); err != nil {
		return err
	}
	var deferredSetup [][]interface{}
	registrations, err := client.register(procedures, &deferredSetup)
	if err != nil {
		return err
	}
	subscription, err := client.subscribe(stateTopic, &deferredSetup)
	if err != nil {
		return err
	}
	service.log("registered %d procedures, subscribed to %s",
		len(registrations), stateTopic)

	sessionContext, stopSession := context.WithCancel(ctx)
	router := make(chan []interface{})
	routerErrors := make(chan error, 1)
	routerDone := make(chan struct{})
	go func() {
		defer close(routerDone)
		defer close(router)
		for _, message := range deferredSetup {
			select {
			case router <- message:
			case <-sessionContext.Done():
				return
			}
		}
		for {
			message, err := client.readFrame()
			if err != nil {
				routerErrors <- err
				return
			}
			select {
			case router <- message:
			case <-sessionContext.Done():
				return
			}
		}
	}()

	defer func() {
		stopSession()
		_ = client.connection.Close()
		<-routerDone
	}()
	ready := service.ready
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ready:
			if err := client.publish(
				sessionTopic,
				[]interface{}{"ready"},
			); err != nil {
				return fmt.Errorf("publish DSP session readiness: %w", err)
			}
			ready = nil
		case err := <-routerErrors:
			return err
		case received, ok := <-service.device:
			if !ok {
				return fmt.Errorf("%w: link closed", errDSPLinkFailure)
			}
			if err := service.handleDeviceFrame(client, received); err != nil {
				return err
			}
		case message, ok := <-router:
			if !ok {
				return errors.New("WAMP connection closed")
			}
			if err := service.handleMessage(
				sessionContext,
				client,
				registrations,
				subscription,
				message,
			); err != nil {
				return err
			}
		}
	}
}

// pump runs the message loop, sleeping when the link reports no traffic just
// as the donor's Dsp_msg_process does.
func (service *wampService) pump(
	ctx context.Context,
	device chan<- frame,
) error {
	idle := service.idle
	if idle <= 0 {
		idle = 200 * time.Millisecond
	}
	for {
		if ctx.Err() != nil {
			return nil
		}
		received, worked, err := service.link.Poll()
		if err != nil {
			if !isFrameError(err) {
				return err
			}
			service.log("discarded frame: %v", err)
		}
		if received != nil {
			if code, ok := received.Code(); ok &&
				eventName(received.ID, code) == "EVENT_DSP_BOOTUP" {
				// The link is the authority for this fact. Record it here so
				// readiness never depends on the frame also reaching a live
				// WAMP session; delivery below is best effort.
				if err := recordDSPBootState(
					service.policy.BootStatePath,
				); err != nil {
					service.log("%v", err)
				}
				select {
				case service.bootEvents <- struct{}{}:
				default:
				}
			}
			select {
			case device <- *received:
			case <-ctx.Done():
				return nil
			default:
				service.log(
					"dropped DSP event id=%d while WAMP delivery was unavailable",
					received.ID,
				)
			}
		}
		if !worked {
			timer := time.NewTimer(idle)
			select {
			case <-timer.C:
			case <-ctx.Done():
				timer.Stop()
				return nil
			}
		}
	}
}

func (service *wampService) handleMessage(
	ctx context.Context,
	client *wampConnection,
	registrations map[uint64]procedureSpec,
	subscription uint64,
	message []interface{},
) error {
	switch messageType(message) {
	case wampInvocation:
		return service.handleInvocation(ctx, client, registrations, message)
	case wampEvent:
		return service.handleEvent(ctx, subscription, message)
	case wampResult, wampError:
		service.log("router response: %v", message)
		return nil
	default:
		return nil
	}
}

func (service *wampService) handleInvocation(
	ctx context.Context,
	client *wampConnection,
	registrations map[uint64]procedureSpec,
	message []interface{},
) error {
	if len(message) < 4 {
		return errors.New("malformed WAMP invocation")
	}
	requestID, ok := unsigned(message[1])
	if !ok {
		return errors.New("WAMP invocation has invalid request ID")
	}
	registrationID, ok := unsigned(message[2])
	if !ok {
		return errors.New("WAMP invocation has invalid registration ID")
	}
	spec, known := registrations[registrationID]
	if !known {
		return nil
	}
	var args []interface{}
	if len(message) > 4 {
		args, _ = message[4].([]interface{})
	}

	invocationError := service.dispatch(ctx, spec, args)
	if invocationError != nil {
		return client.writeFrame([]interface{}{
			wampError,
			wampInvocation,
			requestID,
			map[string]interface{}{},
			"com.harman.error",
			[]interface{}{invocationError.Error()},
		})
	}
	return client.writeFrame([]interface{}{
		wampYield,
		requestID,
		map[string]interface{}{},
		[]interface{}{},
	})
}

// dispatch encodes one procedure call into a frame and queues it.
func (service *wampService) dispatch(
	ctx context.Context,
	spec procedureSpec,
	args []interface{},
) error {
	if service.safeOnly && !isSafeMicrophoneMute(spec, args) {
		return fmt.Errorf("%s is disabled by local policy", spec.Name)
	}
	payload, err := encodeCall(spec, args)
	if err != nil {
		return err
	}
	timeout := service.commandTimeout
	if timeout <= 0 {
		timeout = dspCommandTimeout
	}
	commandContext, cancelCommand := context.WithTimeout(ctx, timeout)
	defer cancelCommand()
	completion, err := service.link.EnqueueTracked(
		commandContext,
		spec.ID,
		payload,
	)
	if err != nil {
		return err
	}
	service.log("queued %s as id %d payload % x", spec.Name, spec.ID, payload)
	select {
	case err := <-completion:
		if err != nil {
			return fmt.Errorf("transmit %s: %w", spec.Name, err)
		}
		service.log("completed %s", spec.Name)
		return nil
	case <-commandContext.Done():
		return commandContext.Err()
	}
}

func isSafeMicrophoneMute(spec procedureSpec, args []interface{}) bool {
	if spec.Opcode != micMuteSpec.Opcode || len(args) != 1 {
		return false
	}
	value, ok := byteArgument(args[0])
	return ok && value == 1
}

// encodeCall builds the payload bytes of a procedure: its opcode followed by
// one byte per argument.
func encodeCall(spec procedureSpec, args []interface{}) ([]byte, error) {
	if len(args) != spec.Arguments {
		return nil, fmt.Errorf(
			"%s takes %d argument(s)",
			spec.Name,
			spec.Arguments,
		)
	}
	payload := make([]byte, 0, spec.Arguments+1)
	payload = append(payload, spec.Opcode)
	for _, argument := range args {
		value, ok := byteArgument(argument)
		if !ok {
			return nil, errors.New("invalid argument format")
		}
		payload = append(payload, value)
	}
	return payload, nil
}

func byteArgument(value interface{}) (byte, bool) {
	switch typed := value.(type) {
	case uint64:
		if typed <= 0xff {
			return byte(typed), true
		}
	case int64:
		if typed >= 0 && typed <= 0xff {
			return byte(typed), true
		}
	case int:
		if typed >= 0 && typed <= 0xff {
			return byte(typed), true
		}
	case float64:
		if typed >= 0 && typed <= 0xff && typed == float64(int64(typed)) {
			return byte(int64(typed)), true
		}
	case bool:
		if typed {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// handleEvent forwards com.harman.stateChanged to the DSP. The owned stack
// publishes a source name on this topic, and no recovered evidence maps a
// source name to the donor's one byte state, so a non numeric state is
// reported and dropped rather than guessed.
func (service *wampService) handleEvent(
	ctx context.Context,
	subscription uint64,
	message []interface{},
) error {
	if len(message) < 4 {
		return nil
	}
	subscriptionID, ok := unsigned(message[1])
	if !ok || subscriptionID != subscription {
		return nil
	}
	var args []interface{}
	if len(message) > 4 {
		args, _ = message[4].([]interface{})
	}
	if len(args) != 1 {
		service.log("ignored %s with %d arguments", stateTopic, len(args))
		return nil
	}
	if _, ok := byteArgument(args[0]); !ok {
		service.log("ignored %s state %v, no recovered byte encoding",
			stateTopic, args[0])
		return nil
	}
	if err := service.dispatch(ctx, stateChangedSpec, args); err != nil {
		service.log("ignored %s: %v", stateTopic, err)
	}
	return nil
}

// handleDeviceFrame turns a received frame into the recovered log line and,
// for the two frames that carry meaning to the rest of the system, a WAMP
// publication.
func (service *wampService) handleDeviceFrame(
	client *wampConnection,
	received frame,
) error {
	code, ok := received.Code()
	if !ok {
		return nil
	}
	name := eventName(received.ID, code)
	if name == "" {
		service.log("readmsg id=%d other event=0x%02x", received.ID, code)
		return nil
	}
	service.log("readmsg id=%d code=0x%02x %s", received.ID, code, name)

	switch name {
	case "EVENT_DSP_VERSION":
		text, packed, ok := decodeVersion(received.Payload)
		if !ok {
			service.log("EVENT_DSP_VERSION payload is too short")
			return nil
		}
		service.log("EVENT_DSP_VERSION=%s", text)
		return client.publish(versionTopic, []interface{}{packed})
	case "EVENT_DSP_BOOTUP":
		return service.reportBootup(client)
	case "DSP_MEMORY_DUMP":
		// The donor appends these to a file. This service writes no persistent
		// storage, so the payload is counted and dropped.
		service.log("discarded %d byte memory dump payload",
			len(received.Payload))
		return nil
	}
	return nil
}

// reportBootup notifies the mute policy owner. It never requests unmute.
func (service *wampService) reportBootup(client *wampConnection) error {
	if err := recordDSPBootState(service.policy.BootStatePath); err != nil {
		return err
	}
	if service.policy.NotifyTopic != "" {
		if err := client.publish(
			service.policy.NotifyTopic,
			[]interface{}{"dsp"},
		); err != nil {
			return err
		}
	}
	if service.policy.PolicyProcedure == "" {
		service.log("DSP booted; mute policy owner not called, outputs unchanged")
		return nil
	}
	service.log("DSP booted; notifying mute policy %s",
		service.policy.PolicyProcedure)
	return client.call(
		service.policy.PolicyProcedure,
		[]interface{}{"dsp-bootup"},
	)
}

func (client *wampConnection) negotiate(realm string) error {
	if _, err := client.connection.Write([]byte{0x7f, 0xf2, 0, 0}); err != nil {
		return fmt.Errorf("write RawSocket handshake: %w", err)
	}
	handshake := make([]byte, 4)
	if _, err := io.ReadFull(client.connection, handshake); err != nil {
		return fmt.Errorf("read RawSocket handshake: %w", err)
	}
	if handshake[0] != 0x7f || handshake[1]&0x0f != 2 {
		return fmt.Errorf("RawSocket handshake rejected: %x", handshake)
	}
	if err := client.writeFrame([]interface{}{
		wampHello,
		realm,
		map[string]interface{}{
			"roles": map[string]interface{}{
				"callee":     map[string]interface{}{},
				"caller":     map[string]interface{}{},
				"publisher":  map[string]interface{}{},
				"subscriber": map[string]interface{}{},
			},
		},
	}); err != nil {
		return err
	}
	response, err := client.readFrame()
	if err != nil {
		return err
	}
	if messageType(response) != wampWelcome {
		return fmt.Errorf("expected WELCOME, received %v", response)
	}
	return nil
}

// awaitSetupResponse reads until the reply to requestID arrives. A router may
// deliver an event or an invocation between a setup request and its reply, so
// anything else is queued for the session loop instead of being mistaken for
// the reply or dropped.
func (client *wampConnection) awaitSetupResponse(
	expected uint64,
	requestID uint64,
	label string,
	deferred *[][]interface{},
) ([]interface{}, error) {
	for {
		message, err := client.readFrame()
		if err != nil {
			return nil, err
		}
		responseRequestID, hasRequestID := uint64(0), false
		if len(message) > 1 {
			responseRequestID, hasRequestID = unsigned(message[1])
		}
		switch messageType(message) {
		case expected:
			if hasRequestID && responseRequestID == requestID {
				return message, nil
			}
		case wampError:
			// An ERROR carries the failing request type then its id.
			if len(message) > 2 {
				if failed, ok := unsigned(message[2]); ok &&
					failed == requestID {
					return nil, fmt.Errorf("%s failed: %v", label, message)
				}
			}
		}
		*deferred = append(*deferred, message)
		if len(*deferred) > maxDeferredSetupMessages {
			return nil, fmt.Errorf(
				"%s: router sent %d messages without replying",
				label,
				len(*deferred),
			)
		}
	}
}

func (client *wampConnection) register(
	specs []procedureSpec,
	deferred *[][]interface{},
) (map[uint64]procedureSpec, error) {
	registrations := make(map[uint64]procedureSpec, len(specs))
	for _, spec := range specs {
		requestID := client.requestID()
		if err := client.writeFrame([]interface{}{
			wampRegister,
			requestID,
			map[string]interface{}{},
			spec.Name,
		}); err != nil {
			return nil, err
		}
		response, err := client.awaitSetupResponse(
			wampRegistered,
			requestID,
			fmt.Sprintf("registration of %s", spec.Name),
			deferred,
		)
		if err != nil {
			return nil, err
		}
		if len(response) < 3 {
			return nil, fmt.Errorf(
				"registration failed for %s: %v",
				spec.Name,
				response,
			)
		}
		registrationID, registrationOK := unsigned(response[2])
		if !registrationOK {
			return nil, fmt.Errorf(
				"invalid registration response for %s",
				spec.Name,
			)
		}
		registrations[registrationID] = spec
	}
	return registrations, nil
}

func (client *wampConnection) subscribe(
	topic string,
	deferred *[][]interface{},
) (uint64, error) {
	requestID := client.requestID()
	if err := client.writeFrame([]interface{}{
		wampSubscribe,
		requestID,
		map[string]interface{}{},
		topic,
	}); err != nil {
		return 0, err
	}
	response, err := client.awaitSetupResponse(
		wampSubscribed,
		requestID,
		fmt.Sprintf("subscription to %s", topic),
		deferred,
	)
	if err != nil {
		return 0, err
	}
	if len(response) < 3 {
		return 0, fmt.Errorf("subscription failed for %s: %v", topic, response)
	}
	subscription, ok := unsigned(response[2])
	if !ok {
		return 0, fmt.Errorf("invalid subscription response for %s", topic)
	}
	return subscription, nil
}

func (client *wampConnection) publish(
	topic string,
	args []interface{},
) error {
	return client.writeFrame([]interface{}{
		wampPublish,
		client.requestID(),
		map[string]interface{}{},
		topic,
		args,
	})
}

// call refuses the physical mute gates, so no code path in this program can
// request an amplifier or DAC unmute.
func (client *wampConnection) call(
	procedure string,
	args []interface{},
) error {
	for _, forbidden := range forbiddenProcedures {
		if procedure == forbidden {
			return fmt.Errorf("refusing to call %s", forbidden)
		}
	}
	return client.writeFrame([]interface{}{
		wampCall,
		client.requestID(),
		map[string]interface{}{},
		procedure,
		args,
	})
}

func (client *wampConnection) requestID() uint64 {
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	id := client.nextID
	client.nextID++
	return id
}

func (client *wampConnection) writeFrame(message []interface{}) error {
	payload, err := encodeMessagePack(message)
	if err != nil {
		return err
	}
	if len(payload) > wampMaxFrameLen {
		return errors.New("WAMP frame exceeds RawSocket limit")
	}
	header := []byte{
		0,
		byte(len(payload) >> 16),
		byte(len(payload) >> 8),
		byte(len(payload)),
	}
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	if err := writeAll(client.connection, header); err != nil {
		return err
	}
	return writeAll(client.connection, payload)
}

func (client *wampConnection) readFrame() ([]interface{}, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(client.connection, header); err != nil {
		return nil, err
	}
	if header[0] != 0 {
		return nil, fmt.Errorf("unsupported RawSocket frame type %d", header[0])
	}
	length := int(header[1])<<16 | int(header[2])<<8 | int(header[3])
	payload := make([]byte, length)
	if _, err := io.ReadFull(client.connection, payload); err != nil {
		return nil, err
	}
	decoded, err := decodeMessagePack(payload)
	if err != nil {
		return nil, err
	}
	message, ok := decoded.([]interface{})
	if !ok {
		return nil, errors.New("WAMP message is not an array")
	}
	return message, nil
}

func writeAll(writer io.Writer, buffer []byte) error {
	for len(buffer) > 0 {
		written, err := writer.Write(buffer)
		if err != nil {
			return err
		}
		buffer = buffer[written:]
	}
	return nil
}

func messageType(message []interface{}) uint64 {
	if len(message) == 0 {
		return 0
	}
	value, _ := unsigned(message[0])
	return value
}

func unsigned(value interface{}) (uint64, bool) {
	switch number := value.(type) {
	case uint64:
		return number, true
	case int64:
		if number >= 0 {
			return uint64(number), true
		}
	case int:
		if number >= 0 {
			return uint64(number), true
		}
	}
	return 0, false
}

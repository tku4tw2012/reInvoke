// Copyright (c) 2026 tku4tw2012
// SPDX-License-Identifier: MIT

const MUSIC_SOURCE = "com.harman.bluetooth";

export function normalizeMusicVolume(value) {
  if (!Number.isFinite(value)) {
    throw new TypeError("volume must be a finite number");
  }
  return Math.max(0, Math.min(100, Math.trunc(value)));
}

function clone(value) {
  return structuredClone(value);
}

export class SpeakerControlState {
  constructor({ musicVolume = 20, musicMuted = false } = {}) {
    this.musicVolume = normalizeMusicVolume(musicVolume);
    this.musicMuted = Boolean(musicMuted);
    this.activeSource = "";
    this.registeredSources = [];
    this.streams = {
      alert: { priority: "5", state: "" },
      "alert-type": { priority: "", state: "" },
      bluetooth: { priority: "5", state: "" },
      call: { state: "" },
      microphone: { priority: "4", state: "" },
      music: { state: "" },
      system: { state: "" },
      voice: { priority: "5", state: "" },
    };
  }

  volumeState() {
    return {
      music: {
        mute: this.musicMuted ? 1 : 0,
        volume: this.musicVolume,
      },
      system: { mute: 0, volume: 70 },
    };
  }

  stateGet() {
    return { args: [], kwargs: clone(this.streams), events: [] };
  }

  volumeGet() {
    return { args: [], kwargs: this.volumeState(), events: [] };
  }

  volumeSet(value) {
    this.musicVolume = normalizeMusicVolume(value);
    return this.#volumeResult(this.musicVolume);
  }

  volumeAdjust(delta) {
    return this.volumeSet(this.musicVolume + clampAdjustment(delta));
  }

  musicMuteSet(muted) {
    this.musicMuted = Boolean(muted);
    const result = {
      args: [this.musicMuted, "music"],
      kwargs: this.volumeState(),
      events: [
        {
          topic: "com.harman.volumeChanged",
          args: ["music", this.musicMuted ? 0 : this.musicVolume],
          kwargs: {},
        },
        {
          topic: "com.harman.musicMuteChanged",
          args: [this.musicMuted],
          kwargs: {},
        },
      ],
    };
    return result;
  }

  musicMuteToggle() {
    return this.musicMuteSet(!this.musicMuted);
  }

  extStateUpdate(stream, state) {
    if (!Object.hasOwn(this.streams, stream)) {
      this.streams[stream] = { state: "" };
    }
    this.streams[stream].state = String(state);
    return {
      args: [],
      kwargs: {},
      events: [
        {
          topic: "com.harman.stateChanged",
          args: [stream],
          kwargs: clone(this.streams),
        },
      ],
    };
  }

  sourceRegister(source) {
    if (!this.registeredSources.includes(source)) {
      this.registeredSources.push(source);
    }
    return { args: [], kwargs: {}, events: [] };
  }

  sourceStart(source) {
    if (!this.registeredSources.includes(source)) {
      throw new Error(`source is not registered: ${source}`);
    }
    this.activeSource = source;
    return { args: [], kwargs: {}, events: [] };
  }

  sourceGetActive() {
    return { args: [this.activeSource], kwargs: {}, events: [] };
  }

  sourceGetRegistered() {
    return { args: [...this.registeredSources], kwargs: {}, events: [] };
  }

  registerBluetoothSource() {
    this.sourceRegister(MUSIC_SOURCE);
    this.sourceStart(MUSIC_SOURCE);
  }

  reconcileBackend(snapshot) {
    if (snapshot === null || typeof snapshot !== "object") {
      throw new TypeError("backend snapshot must be an object");
    }
    if (typeof snapshot.pcmAvailable !== "boolean") {
      throw new TypeError("backend PCM availability must be boolean");
    }
    if (
      snapshot.pcmAvailable &&
      (snapshot.volume === undefined || snapshot.muted === undefined)
    ) {
      throw new TypeError("available PCM snapshot requires volume and mute");
    }
    if (
      snapshot.pcmAvailable &&
      (!Number.isInteger(snapshot.volume) ||
        snapshot.volume < 0 ||
        snapshot.volume > 100)
    ) {
      throw new TypeError("backend volume must be an integer from 0 to 100");
    }
    if (snapshot.pcmAvailable && typeof snapshot.muted !== "boolean") {
      throw new TypeError("backend mute state must be boolean");
    }
    if (
      snapshot.sourceConnected !== undefined &&
      typeof snapshot.sourceConnected !== "boolean"
    ) {
      throw new TypeError("backend source state must be boolean");
    }
    if (
      snapshot.transportState !== undefined &&
      typeof snapshot.transportState !== "string"
    ) {
      throw new TypeError("backend transport state must be string");
    }

    const events = [];
    const previousVolume = this.musicVolume;
    const previousMuted = this.musicMuted;

    if (snapshot.pcmAvailable === true) {
      this.musicVolume = snapshot.volume;
      this.musicMuted = snapshot.muted;

      if (
        previousVolume !== this.musicVolume ||
        previousMuted !== this.musicMuted
      ) {
        events.push({
          topic: "com.harman.volumeChanged",
          args: ["music", this.musicMuted ? 0 : this.musicVolume],
          kwargs: {},
        });
      }
      if (previousMuted !== this.musicMuted) {
        events.push({
          topic: "com.harman.musicMuteChanged",
          args: [this.musicMuted],
          kwargs: {},
        });
      }
    }

    if (snapshot.sourceConnected !== undefined) {
      if (snapshot.sourceConnected) {
        this.registerBluetoothSource();
      } else {
        this.registeredSources = this.registeredSources.filter(
          (source) => source !== MUSIC_SOURCE,
        );
        if (this.activeSource === MUSIC_SOURCE) {
          this.activeSource = "";
        }
      }
    }

    if (
      snapshot.transportState !== undefined &&
      snapshot.transportState !== this.streams.bluetooth.state
    ) {
      events.push(...this.extStateUpdate("bluetooth", snapshot.transportState).events);
    }

    return events;
  }

  #volumeResult(value) {
    return {
      args: [value, "music"],
      kwargs: this.volumeState(),
      events: [
        {
          topic: "com.harman.volumeChanged",
          args: ["music", this.musicMuted ? 0 : value],
          kwargs: {},
        },
      ],
    };
  }
}

function clampAdjustment(value) {
  if (!Number.isFinite(value)) {
    throw new TypeError("volume adjustment must be a finite number");
  }
  return Math.trunc(value);
}

#!/bin/bash

INITRD_BIN_SIZE=$(stat -L -c "%s" 82_IMAGE)

#echo mw.l f7ea8000 09004800
#echo mw.l f7ea8004 00000001
#echo mw.l f7ea8008 09000000
#echo mw.l f7ea800c 00001249
#echo mw.l f7fcd200 00000009

echo -n "set bootargs console=ttyS0,115200 debug init=/bin/sh root=/dev/ram"
echo -n " mtdparts=mv_nand:128K(block0),1M(pre-bootloader),1408K(env),512K(aligned),2M(post-bootloader),2M(post-bootloader),16M(factory_setting),16M(tz_en),16M(tz_en-B),16M(bootimgs),16M(bootimgs-B),192M(rootfs),152M(app),16M(localstorage),64M(BDlocalstorage),1M(bbt)"
echo " initrd=0x08000000,${INITRD_BIN_SIZE}"


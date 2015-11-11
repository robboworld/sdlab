#!/bin/sh

for I in /sys/class/video4linux/*;
do
	[ -e $I ] || continue
	[ -e $I/name ] || continue
	name=`cat $I/name`
	device="/dev/$(basename "$I")"
	echo "$device:$name"
done

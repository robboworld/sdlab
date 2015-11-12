#!/bin/bash

APPNAME="mjpg_streamer"

apppid=`ps aux | grep "[${APPNAME:0:1}]${APPNAME:1}\s*\-b" | awk '{print $2}'`

if [ -n "$apppid" ];
then
	cat -v /proc/${apppid}/cmdline | sed 's/\^@/\n/g' && echo
fi

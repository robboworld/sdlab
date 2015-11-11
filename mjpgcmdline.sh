#!/bin/sh

mjpid=`ps aux | grep "[m]jpg_streamer" | awk '{print $2}'`

if [ -n "$mjpid" ];
then
	cat -v /proc/${mjpid}/cmdline | sed 's/\^@/\n/g' && echo
fi

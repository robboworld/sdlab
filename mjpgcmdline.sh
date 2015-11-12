#! /bin/sh

COLUMNS=9999
MJPGAPPNAME="mjpg_streamer"
mjpgfirst=$(echo $MJPGAPPNAME | awk '{ string=substr($0, 1, 1); print string; }' )
mjpglast=$(echo $MJPGAPPNAME | awk '{ string=substr($0, 2); print string; }' )
mjpgapppid=`ps aux | grep "[$mjpgfirst]$mjpglast\s*\-b" | awk '{ print $2; }'`
if [ -n "$mjpgapppid" ];
then
	cat -v /proc/${mjpgapppid}/cmdline | sed 's/\^@/\n/g' && echo
fi

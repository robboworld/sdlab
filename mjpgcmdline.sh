#! /bin/sh

COLUMNS=9999
APPNAME="mjpg_streamer"
appname_first=$(echo $APPNAME | awk '{ string=substr($0, 1, 1); print string; }' )
appname_last=$(echo $APPNAME | awk '{ string=substr($0, 2); print string; }' )
apppid=`ps aux | grep "[$appname_first]$appname_last\s*\-b" | awk '{ print $2; }'`
if [ -n "$apppid" ];
then
	# Get command line only from first process
	apppid1=$(echo "$apppid" | head -n 1)
	cat -v /proc/${apppid1}/cmdline | sed 's/\^@/\n/g' && echo
fi

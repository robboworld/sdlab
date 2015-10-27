#!/bin/sh
# System reboot

echo "shutdown -r now" | at now + 1 minute 

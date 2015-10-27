#!/bin/sh
# Change TZ by name in command line argument

echo $1 >/etc/timezone
dpkg-reconfigure -f noninteractive tzdata

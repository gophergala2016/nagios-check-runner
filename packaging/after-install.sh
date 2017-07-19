#!/bin/sh

getent group nagios > /dev/null || groupadd --system nagios
useradd --system ncr --home /opt/ncr --groups nagios

if [ -x /bin/systemctl ]; then
	/bin/systemctl daemon-reload
	/bin/systemctl start ncr.service
	/bin/systemctl enable ncr.service
elif [ -x /usr/sbin/service ]; then
	/sbin/initctl reload-configuration
	/usr/sbin/service ncr start
fi

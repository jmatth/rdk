#!/bin/sh
cd `dirname $0`

go build -gcflags="all=-N -l" ./
exec ./complexmodule $@

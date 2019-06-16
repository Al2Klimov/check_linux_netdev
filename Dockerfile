FROM golang:1.11 as build

RUN go get github.com/golang/dep \
	&& go install github.com/golang/dep/...

ADD . /go/src/github.com/Al2Klimov/check_linux_netdev

RUN cd /go/src/github.com/Al2Klimov/check_linux_netdev \
	&& /go/bin/dep ensure \
	&& go generate \
	&& go install .

FROM grandmaster/check-plugins-demo

COPY --from=build /go/bin/check_linux_netdev /usr/lib/nagios/plugins/
COPY icinga2/check_linux_netdev.conf icinga2/check_linux_netdev-service.conf _docker/icinga2-monobjs.conf /etc/icinga2/conf.d/

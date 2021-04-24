build: podcast-proxy

podcast-proxy: podcast-proxy.go
	go build

install: build
	install -D podcast-proxy /usr/bin/podcast-proxy
	install -D podcast-proxy.service /usr/lib/systemd/system/podcast-proxy.service
	adduser --home /var/lib/podcast-proxy --disabled-password --disabled-login podcast-proxy

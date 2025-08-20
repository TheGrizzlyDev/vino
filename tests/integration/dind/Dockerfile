FROM golang:1.25-alpine as build
WORKDIR /src
COPY . .
RUN go build -o ./vinoc ./cmd/vinoc

FROM docker:dind
COPY --from=build /src/vinoc /usr/local/sbin/containerd-shim-vinoc-v1
RUN chmod 777 /usr/local/sbin/containerd-shim-vinoc-v1
RUN mkdir -p /etc/docker
COPY tests/integration/dind/daemon.json /etc/docker/daemon.json
CMD []

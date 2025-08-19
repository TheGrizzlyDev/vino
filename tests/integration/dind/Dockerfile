FROM golang:1.25 as build
WORKDIR /src
COPY . .
RUN go build -o ./vinoc ./cmd/vinoc

FROM docker:dind
COPY --from=build /src/vinoc /usr/local/sbin/vinoc
RUN mkdir -p /etc/docker
COPY tests/integration/dind/daemon.json /etc/docker/daemon.json
CMD []

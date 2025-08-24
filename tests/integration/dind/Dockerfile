FROM golang:1.25-alpine as build
WORKDIR /src
COPY . .
RUN go build -o ./delegatec ./cmd/delegatec

FROM docker:dind
RUN apk add curl
COPY --from=build /src/delegatec /usr/local/sbin/delegatec
RUN chmod 777 /usr/local/sbin/delegatec
RUN mkdir -p /etc/docker
COPY tests/integration/dind/daemon.json /etc/docker/daemon.json
CMD []

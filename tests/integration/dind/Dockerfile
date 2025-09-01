FROM golang:1.25-alpine as build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd/delegatec cmd/delegatec
COPY cmd/vino cmd/vino
COPY internal internal
RUN go build -o ./delegatec ./cmd/delegatec
RUN go build -o ./vino ./cmd/vino

FROM docker:dind
RUN apk add curl util-linux
RUN apk add criu --repository=http://dl-cdn.alpinelinux.org/alpine/edge/testing/
COPY --from=build /src/delegatec /usr/local/sbin/delegatec
COPY --from=build /src/vino /usr/local/sbin/vino
RUN chmod 777 /usr/local/sbin/delegatec
RUN chmod 777 /usr/local/sbin/vino
RUN mkdir -p /etc/docker
COPY tests/integration/dind/daemon.json /etc/docker/daemon.json
CMD []

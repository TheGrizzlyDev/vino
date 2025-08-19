FROM golang:1.25 as build
WORKDIR /src
COPY . .
RUN go build -o ./vinoc ./cmd/vinoc

FROM docker:dind
COPY --from=build /src/vinoc /usr/local/sbin/vinoc
COPY tests/integration/dind/entrypoint.sh /usr/local/bin/entrypoint.sh
RUN chmod +x /usr/local/bin/entrypoint.sh
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
CMD []

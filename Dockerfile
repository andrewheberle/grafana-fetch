FROM golang:1.22@sha256:3589439790974ec05491b66b656bf1048d0f50dd010a903463e3156ba1fc26de AS build

COPY . /build/
RUN cd /build && go build ./cmd/grafana-fetch

FROM gcr.io/distroless/base-debian12:latest@sha256:6ae5fe659f28c6afe9cc2903aebc78a5c6ad3aaa3d9d0369760ac6aaea2529c8

COPY --from=build /build/grafana-fetch /app/grafana-fetch

VOLUME [ "/cache" ]

ENV GF_FETCH_CACHE="/cache"

EXPOSE 8080

ENTRYPOINT [ "/app/grafana-fetch" ]
CMD [ "server" ]

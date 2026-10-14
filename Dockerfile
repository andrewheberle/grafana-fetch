FROM golang:1.27@sha256:820153096fda4de6ac1b7194d12e416b7dd8f4e9a33d17e0345b111b945162d8 AS build

COPY . /build/
RUN cd /build && go build ./cmd/grafana-fetch

FROM gcr.io/distroless/base-debian12:latest@sha256:786007f631d22e8a1a5084c5b177352d9dcac24b1e8c815187750f70b24a9fc6

COPY --from=build /build/grafana-fetch /app/grafana-fetch

VOLUME [ "/cache" ]

ENV GF_FETCH_CACHE="/cache"

EXPOSE 8080

ENTRYPOINT [ "/app/grafana-fetch" ]
CMD [ "server" ]

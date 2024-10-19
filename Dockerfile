FROM golang:1.23@sha256:ad5c126b5cf501a8caef751a243bb717ec204ab1aa56dc41dc11be089fafcb4f AS build

COPY . /build/
RUN cd /build && go build ./cmd/grafana-fetch

FROM gcr.io/distroless/base-debian12:latest@sha256:786007f631d22e8a1a5084c5b177352d9dcac24b1e8c815187750f70b24a9fc6

COPY --from=build /build/grafana-fetch /app/grafana-fetch

VOLUME [ "/cache" ]

ENV GF_FETCH_CACHE="/cache"

EXPOSE 8080

ENTRYPOINT [ "/app/grafana-fetch" ]
CMD [ "server" ]

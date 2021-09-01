FROM golang:1.16 AS build

COPY . /build/
RUN cd /build && go build -o /grafana-fetch .

FROM gcr.io/distroless/base-debian10

COPY --from=build /grafana-fetch /

VOLUME [ "/cache" ]

ENV GF_FETCH_CACHE="/cache"

EXPOSE 8080

ENTRYPOINT [ "/grafana-fetch" ]
CMD [ "server" ]

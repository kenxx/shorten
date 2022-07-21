FROM golang:1.18-alpine

WORKDIR /data

COPY . /data

RUN go build -o shorten ./cmd/shorten/shorten.go

FROM alpine:3.16

ENV SHORTEN_PREFIX=/etc/shorten

COPY --from=0 /data/shorten /usr/bin/shorten

RUN chmod +x /usr/bin/shorten
RUN mkdir /etc/shorten

COPY --from=0 /data/public /etc/shorten/public

CMD shorten
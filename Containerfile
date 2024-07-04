FROM docker.io/library/golang:1.22.4-alpine3.20 AS build-env
ADD . /src
RUN cd /src && go build -o ntfy-to-slack

FROM docker.io/library/alpine:3.20.1
WORKDIR /app
COPY --from=build-env /src/ntfy-to-slack /app/
ENTRYPOINT ./ntfy-to-slack
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY *.go SKILL.md ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /shttodo .

FROM gcr.io/distroless/static-debian12
COPY --from=build /shttodo /shttodo
ENV SHTTODO_DATABASE_PATH=/data/shttodo.db
VOLUME /data
EXPOSE 8080
ENTRYPOINT ["/shttodo"]
CMD ["serve"]

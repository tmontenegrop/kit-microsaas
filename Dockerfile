FROM golang:1.25-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /app/server ./cmd

FROM alpine:3.21
RUN apk add --no-cache ca-certificates tzdata
WORKDIR /app
COPY --from=build /app/server .
COPY --from=build /app/views/ ./views/
COPY --from=build /app/static/ ./static/
COPY --from=build /app/db/migrations/ ./db/migrations/
EXPOSE 8090
VOLUME ["/app/data"]
ENV ENV=production
CMD ["./server"]

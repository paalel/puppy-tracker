FROM golang:1.22-alpine AS build
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o puppy .

FROM alpine:3.19
RUN apk add --no-cache sqlite
COPY --from=build /app/puppy /usr/local/bin/puppy
EXPOSE 8080
CMD ["puppy"]

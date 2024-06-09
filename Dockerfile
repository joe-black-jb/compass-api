FROM golang:1.22.3-alpine

# Install air for hot reload
RUN go install github.com/air-verse/air@latest

WORKDIR /app

# pre-copy/cache go.mod for pre-downloading dependencies and only redownloading them in subsequent builds if they change
COPY go.mod go.sum .air.toml ./

# Download dependencies
RUN go mod download && go mod verify

COPY . .

CMD ["air"]
# Stage 1: Build stage
FROM golang:1.22.2-alpine AS builder

WORKDIR /app

# Copy go mod and sum files
COPY go.mod go.sum ./

# Download dependencies
RUN go mod tidy

# Copy the source code and other files, including the .env file
COPY . .

# Build the Go application
RUN go build -o main .

# Stage 2: Final stage
FROM alpine:3.18.4

WORKDIR /app

# Copy everything from the /app directory of the builder stage to the final stage
COPY --from=builder /app .

# Ensure the binary is executable
RUN chmod +x main

# Expose the port the application runs on 
EXPOSE 9000

# Run the application
CMD ["./main"]

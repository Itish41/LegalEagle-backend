# Build Stage
FROM golang:1.24.0 AS builder

# Set the working directory
WORKDIR /app

# Copy go.mod and go.sum
COPY go.mod go.sum ./

# Install dependencies (matches your local `go mod tidy`)
RUN go mod tidy

# Copy the rest of the application files 
COPY . .

# Copy the .env file
COPY .env ./

# Build the application, explicitly naming the output and ensuring static linking
RUN CGO_ENABLED=0 go build -o LegalEagle

# Final stage
FROM alpine:latest

# Set the working directory
WORKDIR /app

# Copy the binary from the builder stage
COPY --from=builder /app/LegalEagle .

# Copy the .env file
COPY .env ./

# Copy the migration files
COPY db/migrations ./db/migrations

# Expose the application port
EXPOSE 8080

# Command to run the application
CMD ["./LegalEagle"]
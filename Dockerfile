# Start from the official Go image
FROM golang:1.20-alpine

# Set the working directory
WORKDIR /app

# Copy go.mod file
COPY go.mod .

# Download all dependencies and generate go.sum
RUN go mod download && go mod verify

# Copy the source code
COPY . .

# Build the application
RUN go build -o main .

# Expose port 8080
EXPOSE 8080

# Run the executable
CMD ["./main"]
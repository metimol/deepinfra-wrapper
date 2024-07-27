# Start from the official Go image
FROM golang:1.20-alpine

# Set the working directory
WORKDIR /app

# Copy the source code
COPY . .

# Download all dependencies
RUN go mod download

# Build the application
RUN go build -o main .

# Expose port 8080
EXPOSE 8080

# Run the executable
CMD ["./main"]
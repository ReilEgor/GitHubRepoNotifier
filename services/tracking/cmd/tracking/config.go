package main

// Config holds the settings of the tracking service, read from environment variables.
type Config struct {
	HealthPort string `env:"WORKER_HEALTH_PORT"   envDefault:"8081"`
	GRPCPort   string `env:"APP_GRPC_PORT"        envDefault:"9090"`
	APIKey     string `env:"APP_API_KEY,required"`
}

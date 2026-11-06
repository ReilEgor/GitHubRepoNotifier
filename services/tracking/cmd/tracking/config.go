package main

import "time"

// Config holds the settings of the tracking service, read from environment variables.
type Config struct {
	HealthPort string `env:"WORKER_HEALTH_PORT"   envDefault:"8081"`
	GRPCPort   string `env:"APP_GRPC_PORT"        envDefault:"9090"`
	APIKey     string `env:"APP_API_KEY,required"`
	DBURL      string `env:"DB_URL,required"`

	RedisHost     string `env:"REDIS_HOST,required"`
	RedisPort     string `env:"REDIS_PORT"          envDefault:"6379"`
	RedisPassword string `env:"REDIS_PASSWORD"`

	GitHubAPIURL      string        `env:"GITHUB_API_URL"      envDefault:"https://api.github.com"`
	GitHubToken       string        `env:"GITHUB_TOKEN"`
	GitHubHTTPTimeout time.Duration `env:"GITHUB_HTTP_TIMEOUT" envDefault:"3s"`
}

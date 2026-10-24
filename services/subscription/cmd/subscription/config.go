package main

// Config holds the settings of the subscription service, read from environment variables.
type Config struct {
	HTTPPort         string `env:"APP_HTTP_PORT"        envDefault:"8080"`
	DBURL            string `env:"DB_URL,required"`
	APIKey           string `env:"APP_API_KEY,required"`
	TrackingGRPCAddr string `env:"TRACKING_GRPC_ADDR"   envDefault:"localhost:9090"`
	RedisHost        string `env:"REDIS_HOST,required"`
	RedisPort        string `env:"REDIS_PORT"           envDefault:"6379"`
	RedisPassword    string `env:"REDIS_PASSWORD"`
	RateLimit        string `env:"RATE_LIMIT"           envDefault:"10-S"`
}

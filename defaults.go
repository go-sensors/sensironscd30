package sensironscd30

import (
	"time"

	"github.com/go-sensors/core/i2c"
)

const (
	DefaultReconnectTimeout = 5 * time.Second
)

// GetDefaultI2CPortConfig gets the manufacturer-specified defaults for connecting to the sensor
func GetDefaultI2CPortConfig() *i2c.I2CPortConfig {
	return &i2c.I2CPortConfig{
		Address: 0x61,
	}
}

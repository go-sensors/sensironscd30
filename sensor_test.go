package sensironscd30_test

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/go-sensors/core/gas"
	"github.com/go-sensors/core/humidity"
	"github.com/go-sensors/core/io/mocks"
	"github.com/go-sensors/core/temperature"
	"github.com/go-sensors/core/units"
	"github.com/go-sensors/sensironscd30"
	"github.com/golang/mock/gomock"
	"github.com/sigurn/crc8"
	"github.com/stretchr/testify/assert"
	"golang.org/x/sync/errgroup"
)

func Test_NewSensor_returns_a_configured_sensor(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	// Act
	sensor := sensironscd30.NewSensor(portFactory)

	// Assert
	assert.NotNil(t, sensor)
	assert.Equal(t, sensironscd30.DefaultReconnectTimeout, sensor.ReconnectTimeout())
	assert.Nil(t, sensor.RecoverableErrorHandler())
}

func Test_NewSensor_with_options_returns_a_configured_sensor(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)
	expectedReconnectTimeout := sensironscd30.DefaultReconnectTimeout * 10

	// Act
	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithReconnectTimeout(expectedReconnectTimeout),
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	// Assert
	assert.NotNil(t, sensor)
	assert.Equal(t, expectedReconnectTimeout, sensor.ReconnectTimeout())
	assert.NotNil(t, sensor.RecoverableErrorHandler())
	assert.True(t, sensor.RecoverableErrorHandler()(nil))
}

func Test_ConcentrationSpecs_returns_supported_concentrations(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)
	sensor := sensironscd30.NewSensor(portFactory)
	expected := []*gas.ConcentrationSpec{
		{
			Gas:              sensironscd30.CarbonDioxide,
			Resolution:       10 * units.PartPerMillion,
			MinConcentration: 400 * units.PartPerMillion,
			MaxConcentration: 10000 * units.PartPerMillion,
		},
	}

	// Act
	actual := sensor.ConcentrationSpecs()

	// Assert
	assert.NotNil(t, actual)
	assert.EqualValues(t, expected, actual)
}

func Test_TemperatureSpecs_returns_supported_temperatures(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)
	sensor := sensironscd30.NewSensor(portFactory)
	expected := []*temperature.TemperatureSpec{
		{
			Resolution:     100 * units.ThousandthDegreeCelsius,
			MinTemperature: -40 * units.DegreeCelsius,
			MaxTemperature: 70 * units.DegreeCelsius,
		},
	}

	// Act
	actual := sensor.TemperatureSpecs()

	// Assert
	assert.NotNil(t, actual)
	assert.EqualValues(t, expected, actual)
}

func Test_RelativeHumiditySpecs_returns_supported_relative_humidities(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)
	sensor := sensironscd30.NewSensor(portFactory)
	expected := []*humidity.RelativeHumiditySpec{
		{
			PercentageResolution: 0.001,
			MinPercentage:        0.0,
			MaxPercentage:        1.0,
		},
	}

	// Act
	actual := sensor.RelativeHumiditySpecs()

	// Assert
	assert.NotNil(t, actual)
	assert.EqualValues(t, expected, actual)
}

func Test_Run_fails_when_opening_port(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)
	portFactory.EXPECT().
		Open().
		Return(nil, errors.New("boom"))
	sensor := sensironscd30.NewSensor(portFactory)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.ErrorContains(t, err, "failed to open port")
}

func Test_Run_fails_to_soft_reset(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	port := mocks.NewMockPort(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Close().
		Return(nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.ErrorContains(t, err, "failed to reset sensor")
}

func Test_Run_fails_to_stop_continuous_measurement(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	port := mocks.NewMockPort(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x01, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Close().
		Return(nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.ErrorContains(t, err, "failed to stop continuous measurement")
}

func Test_Run_fails_to_trigger_continuous_measurement(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	port := mocks.NewMockPort(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x01, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x00, 0x10, 0x00, 0x00, 0x81}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Close().
		Return(nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.ErrorContains(t, err, "failed to trigger continuous measurement")
}

func Test_Run_fails_to_set_measurement_interval(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	port := mocks.NewMockPort(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x01, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x00, 0x10, 0x00, 0x00, 0x81}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x46, 0x00, 0x00, 0x02, 0xE3}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Close().
		Return(nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.ErrorContains(t, err, "failed to set measurement interval")
}

func Test_Run_fails_to_get_data_ready_status(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	port := mocks.NewMockPort(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x01, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x00, 0x10, 0x00, 0x00, 0x81}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x46, 0x00, 0x00, 0x02, 0xE3}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x02, 0x02}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Close().
		Return(nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.ErrorContains(t, err, "failed to get data ready status")
}

func Test_Run_fails_to_read_data_ready_status(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	port := mocks.NewMockPort(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x01, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x00, 0x10, 0x00, 0x00, 0x81}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x46, 0x00, 0x00, 0x02, 0xE3}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x02, 0x02}).
		Return(0, nil)
	port.EXPECT().
		Read(gomock.Any()).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Close().
		Return(nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.ErrorContains(t, err, "failed to get data ready status")
	assert.ErrorContains(t, err, "failed to read data ready status")
}

var (
	checksumTable = crc8.MakeTable(crc8.Params{
		Poly:   0x31,
		Init:   0xFF,
		RefIn:  false,
		RefOut: false,
		XorOut: 0x00,
		Check:  0x00,
		Name:   "CRC-8/Sensiron",
	})
)

func Test_Run_fails_to_validate_crc_when_reading_data_ready_status(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	port := mocks.NewMockPort(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x01, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x00, 0x10, 0x00, 0x00, 0x81}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x46, 0x00, 0x00, 0x02, 0xE3}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x02, 0x02}).
		Return(0, nil)
	port.EXPECT().
		Read(gomock.Any()).
		DoAndReturn(func(buf []byte) (int, error) {
			return len(buf), nil
		})
	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Close().
		Return(nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.ErrorContains(t, err, "failed to get data ready status")
	assert.ErrorContains(t, err, "failed to validate crc")
}

func Test_Run_fails_to_read_measurement(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	port := mocks.NewMockPort(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x01, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x00, 0x10, 0x00, 0x00, 0x81}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x46, 0x00, 0x00, 0x02, 0xE3}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x02, 0x02}).
		Return(0, nil)
	port.EXPECT().
		Read(gomock.Any()).
		DoAndReturn(func(buf []byte) (int, error) {
			buf[0] = 0x00 // MSB
			buf[1] = 0x01 // LSB
			buf[2] = crc8.Checksum(buf[0:2], checksumTable)

			return len(buf), nil
		})
	port.EXPECT().
		Write([]byte{0x03, 0x00}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Close().
		Return(nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.ErrorContains(t, err, "failed to read measurement")
}

func Test_Run_fails_to_read_measurement_values(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	port := mocks.NewMockPort(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x01, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x00, 0x10, 0x00, 0x00, 0x81}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x46, 0x00, 0x00, 0x02, 0xE3}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x02, 0x02}).
		Return(0, nil)
	port.EXPECT().
		Read(gomock.Any()).
		DoAndReturn(func(buf []byte) (int, error) {
			buf[0] = 0x00 // MSB
			buf[1] = 0x01 // LSB
			buf[2] = crc8.Checksum(buf[0:2], checksumTable)

			return len(buf), nil
		})
	port.EXPECT().
		Write([]byte{0x03, 0x00}).
		Return(0, nil)
	port.EXPECT().
		Read(gomock.Any()).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Close().
		Return(nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.ErrorContains(t, err, "failed to read measurement")
	assert.ErrorContains(t, err, "failed to read measurements")
}

func Test_Run_fails_to_validate_crc_while_reading_measurement_values(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	port := mocks.NewMockPort(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x01, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x00, 0x10, 0x00, 0x00, 0x81}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x46, 0x00, 0x00, 0x02, 0xE3}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x02, 0x02}).
		Return(0, nil)
	port.EXPECT().
		Read(gomock.Any()).
		DoAndReturn(func(buf []byte) (int, error) {
			buf[0] = 0x00 // MSB
			buf[1] = 0x01 // LSB
			buf[2] = crc8.Checksum(buf[0:2], checksumTable)

			return len(buf), nil
		})
	port.EXPECT().
		Write([]byte{0x03, 0x00}).
		Return(0, nil)
	port.EXPECT().
		Read(gomock.Any()).
		DoAndReturn(func(buf []byte) (int, error) {
			return len(buf), nil
		})
	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Close().
		Return(nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.ErrorContains(t, err, "failed to read measurement")
	assert.ErrorContains(t, err, "failed to validate crc")
}

func Test_Run_returns_expected_measurements(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)
	portFactory := mocks.NewMockPortFactory(ctrl)

	port := mocks.NewMockPort(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x01, 0x04}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x00, 0x10, 0x00, 0x00, 0x81}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x46, 0x00, 0x00, 0x02, 0xE3}).
		Return(0, nil)
	port.EXPECT().
		Write([]byte{0x02, 0x02}).
		Return(0, nil)
	port.EXPECT().
		Read(gomock.Any()).
		DoAndReturn(func(buf []byte) (int, error) {
			buf[0] = 0x00 // MSB
			buf[1] = 0x01 // LSB
			buf[2] = crc8.Checksum(buf[0:2], checksumTable)

			return len(buf), nil
		})
	port.EXPECT().
		Write([]byte{0x03, 0x00}).
		Return(0, nil)
	expectedConcentration := gas.Concentration{
		Gas:    sensironscd30.CarbonDioxide,
		Amount: 439 * units.PartPerMillion,
	}
	expectedTemperature := 27_200 * units.ThousandthDegreeCelsius
	expectedRelativeHumidity := units.RelativeHumidity{
		Temperature: expectedTemperature,
		Percentage:  0.488,
	}
	port.EXPECT().
		Read(gomock.Any()).
		DoAndReturn(func(buf []byte) (int, error) {
			val := make([]byte, 4)
			binary.BigEndian.PutUint32(val[:], math.Float32bits(float32(expectedConcentration.Amount.PartsPerMillion())))
			buf[0] = val[0]
			buf[1] = val[1]
			buf[2] = crc8.Checksum(buf[0:2], checksumTable)
			buf[3] = val[2]
			buf[4] = val[3]
			buf[5] = crc8.Checksum(buf[3:5], checksumTable)

			binary.BigEndian.PutUint32(val, math.Float32bits(float32(expectedTemperature.DegreesCelsius())))
			buf[6] = val[0]
			buf[7] = val[1]
			buf[8] = crc8.Checksum(buf[6:8], checksumTable)
			buf[9] = val[2]
			buf[10] = val[3]
			buf[11] = crc8.Checksum(buf[9:11], checksumTable)

			binary.BigEndian.PutUint32(val, math.Float32bits(float32(expectedRelativeHumidity.Percentage*100)))
			buf[12] = val[0]
			buf[13] = val[1]
			buf[14] = crc8.Checksum(buf[12:14], checksumTable)
			buf[15] = val[2]
			buf[16] = val[3]
			buf[17] = crc8.Checksum(buf[15:17], checksumTable)

			return len(buf), nil
		})
	port.EXPECT().
		Write([]byte{0xD3, 0x04}).
		Return(0, errors.New("boom"))
	port.EXPECT().
		Close().
		Return(nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return true }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	group.Go(func() error {
		select {
		case actualConcentration, ok := <-sensor.Concentrations():
			assert.True(t, ok)
			assert.NotNil(t, actualConcentration)
			assert.Equal(t, expectedConcentration.Gas, actualConcentration.Gas)
			assert.InEpsilon(t, float64(expectedConcentration.Amount), float64(actualConcentration.Amount), float64(10*units.PartPerMillion))
		case <-time.After(3 * time.Second):
			assert.Fail(t, "failed to receive concentration in expected amount of time")
		}

		select {
		case actualTemperature, ok := <-sensor.Temperatures():
			assert.True(t, ok)
			assert.NotNil(t, actualTemperature)
			assert.Equal(t, expectedTemperature, *actualTemperature)
		case <-time.After(3 * time.Second):
			assert.Fail(t, "failed to receive temperature in expected amount of time")
		}

		select {
		case actualRelativeHumidity, ok := <-sensor.RelativeHumidities():
			assert.True(t, ok)
			assert.NotNil(t, actualRelativeHumidity)
			assert.Equal(t, expectedRelativeHumidity.Temperature, actualRelativeHumidity.Temperature)
			assert.InEpsilon(t, expectedRelativeHumidity.Percentage, actualRelativeHumidity.Percentage, 0.001)
		case <-time.After(3 * time.Second):
			assert.Fail(t, "failed to receive relative humidity in expected amount of time")
		}

		cancel()
		return nil
	})
	err := group.Wait()

	// Assert
	assert.Nil(t, err)
}

func Test_Run_attempts_to_recover_from_failure(t *testing.T) {
	// Arrange
	ctrl := gomock.NewController(t)

	port := mocks.NewMockPort(ctrl)
	port.EXPECT().
		Write(gomock.Any()).
		Return(0, errors.New("boom")).
		AnyTimes()
	port.EXPECT().
		Close().
		Times(1)

	portFactory := mocks.NewMockPortFactory(ctrl)
	portFactory.EXPECT().
		Open().
		Return(port, nil)

	sensor := sensironscd30.NewSensor(portFactory,
		sensironscd30.WithRecoverableErrorHandler(func(err error) bool { return false }))

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	group, ctx := errgroup.WithContext(ctx)

	// Act
	group.Go(func() error {
		return sensor.Run(ctx)
	})
	err := group.Wait()

	// Assert
	assert.Nil(t, err)
}

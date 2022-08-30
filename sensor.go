package sensironscd30

import (
	"context"
	"sync"
	"time"

	"github.com/go-sensors/core/gas"
	"github.com/go-sensors/core/humidity"
	coreio "github.com/go-sensors/core/io"
	"github.com/go-sensors/core/temperature"
	"github.com/go-sensors/core/units"
	"github.com/pkg/errors"
	"golang.org/x/sync/errgroup"
)

const (
	CarbonDioxide string = "CO2"
)

// Sensor represents a configured Sensiron SCD30 gas sensor
type Sensor struct {
	gases              chan *gas.Concentration
	temperatures       chan *units.Temperature
	relativeHumidities chan *units.RelativeHumidity
	portFactory        coreio.PortFactory
	reconnectTimeout   time.Duration
	errorHandlerFunc   ShouldTerminate
	pressure           units.Pressure
	pressureMutex      *sync.Mutex
}

// Option is a configured option that may be applied to a Sensor
type Option struct {
	apply func(*Sensor)
}

// NewSensor creates a Sensor with optional configuration
func NewSensor(portFactory coreio.PortFactory, options ...*Option) *Sensor {
	gases := make(chan *gas.Concentration)
	temperatures := make(chan *units.Temperature)
	relativeHumidities := make(chan *units.RelativeHumidity)
	pressureMutex := &sync.Mutex{}
	s := &Sensor{
		gases:              gases,
		temperatures:       temperatures,
		relativeHumidities: relativeHumidities,
		portFactory:        portFactory,
		reconnectTimeout:   DefaultReconnectTimeout,
		errorHandlerFunc:   nil,
		pressure:           0 * units.Pascal,
		pressureMutex:      pressureMutex,
	}
	for _, o := range options {
		o.apply(s)
	}
	return s
}

// WithReconnectTimeout specifies the duration to wait before reconnecting after a recoverable error
func WithReconnectTimeout(timeout time.Duration) *Option {
	return &Option{
		apply: func(s *Sensor) {
			s.reconnectTimeout = timeout
		},
	}
}

// ReconnectTimeout is the duration to wait before reconnecting after a recoverable error
func (s *Sensor) ReconnectTimeout() time.Duration {
	return s.reconnectTimeout
}

// ShouldTerminate is a function that returns a result indicating whether the Sensor should terminate after a recoverable error
type ShouldTerminate func(error) bool

// WithRecoverableErrorHandler registers a function that will be called when a recoverable error occurs
func WithRecoverableErrorHandler(f ShouldTerminate) *Option {
	return &Option{
		apply: func(s *Sensor) {
			s.errorHandlerFunc = f
		},
	}
}

// RecoverableErrorHandler a function that will be called when a recoverable error occurs
func (s *Sensor) RecoverableErrorHandler() ShouldTerminate {
	return s.errorHandlerFunc
}

const (
	setValueTimeout     time.Duration = 10 * time.Millisecond
	readValueTimeout    time.Duration = 12 * time.Millisecond
	measurementInterval time.Duration = 2 * time.Second
)

// Run begins reading from the sensor and blocks until either an error occurs or the context is completed
func (s *Sensor) Run(ctx context.Context) error {
	defer close(s.gases)
	defer close(s.temperatures)
	defer close(s.relativeHumidities)
	for {
		port, err := s.portFactory.Open()
		if err != nil {
			return errors.Wrap(err, "failed to open port")
		}

		group, innerCtx := errgroup.WithContext(ctx)
		group.Go(func() error {
			<-innerCtx.Done()
			return port.Close()
		})
		group.Go(func() error {
			s.pressureMutex.Lock()
			initialPressure := s.pressure
			s.pressureMutex.Unlock()

			err = triggerContinuousMeasurement(innerCtx, port, initialPressure)
			if err != nil {
				return errors.Wrap(err, "failed to trigger continuous measurement")
			}

			err = setMeasurementInterval(innerCtx, port, measurementInterval)
			if err != nil {
				return errors.Wrap(err, "failed to set measurement interval")
			}

			for {
				s.pressureMutex.Lock()
				pressure := s.pressure
				s.pressureMutex.Unlock()

				ready, err := getDataReadyStatus(ctx, port)
				if err != nil {
					return errors.Wrap(err, "failed to get data ready status")
				}

				if !ready {
					continue
				}

				readings, err := readMeasurement(ctx, port, pressure)
				if err != nil {
					return errors.Wrap(err, "failed to read measurement")
				}

				co2 := &gas.Concentration{
					Gas:    CarbonDioxide,
					Amount: readings.CO2,
				}

				select {
				case <-ctx.Done():
					return nil
				case s.gases <- co2:
				}

				select {
				case <-ctx.Done():
					return nil
				case s.temperatures <- &readings.Temperature:
				}

				select {
				case <-ctx.Done():
					return nil
				case s.relativeHumidities <- &readings.RelativeHumidity:
				}

				select {
				case <-ctx.Done():
					return nil
				case <-time.After(measurementInterval):
				}
			}
		})

		err = group.Wait()
		if s.errorHandlerFunc != nil {
			if s.errorHandlerFunc(err) {
				return err
			}
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(s.reconnectTimeout):
		}
	}
}

// Concentrations returns a channel of concentration readings as they become available from the sensor
func (s *Sensor) Concentrations() <-chan *gas.Concentration {
	return s.gases
}

// ConcentrationSpecs returns a collection of specified measurement ranges supported by the sensor
func (*Sensor) ConcentrationSpecs() []*gas.ConcentrationSpec {
	return []*gas.ConcentrationSpec{
		{
			Gas:              CarbonDioxide,
			Resolution:       10 * units.PartPerMillion,
			MinConcentration: 400 * units.PartPerMillion,
			MaxConcentration: 10000 * units.PartPerMillion,
		},
	}
}

// Temperatures returns a channel of temperature readings as they become available from the sensor
func (s *Sensor) Temperatures() <-chan *units.Temperature {
	return s.temperatures
}

// TemperatureSpecs returns a collection of specified measurement ranges supported by the sensor
func (*Sensor) TemperatureSpecs() []*temperature.TemperatureSpec {
	return []*temperature.TemperatureSpec{
		{
			Resolution:     100 * units.ThousandthDegreeCelsius,
			MinTemperature: -40 * units.DegreeCelsius,
			MaxTemperature: 70 * units.DegreeCelsius,
		},
	}
}

// RelativeHumidities returns a channel of relative humidity readings as they become available from the sensor
func (s *Sensor) RelativeHumidities() <-chan *units.RelativeHumidity {
	return s.relativeHumidities
}

// HumiditySpecs returns a collection of specified measurement ranges supported by the sensor
func (*Sensor) RelativeHumiditySpecs() []*humidity.RelativeHumiditySpec {
	return []*humidity.RelativeHumiditySpec{
		{
			PercentageResolution: 0.001,
			MinPercentage:        0.0,
			MaxPercentage:        1.0,
		},
	}
}

func (s *Sensor) HandlePressure(ctx context.Context, pressure *units.Pressure) error {
	s.pressureMutex.Lock()
	defer s.pressureMutex.Unlock()

	s.pressure = *pressure

	return nil
}

// This package provides an implementation to read gas concentration measurements from a Sensiron SCD30 sensor.
package sensironscd30

import (
	"context"
	"encoding/binary"
	"math"
	"time"

	coreio "github.com/go-sensors/core/io"
	"github.com/go-sensors/core/units"
	"github.com/pkg/errors"
	"github.com/sigurn/crc8"
)

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

func numberToWord(number float64) (byte, byte, byte) {
	value := uint16(number)
	msb := byte((value >> 8) & 0xFF)
	lsb := byte(value & 0xFF)
	crc := crc8.Checksum([]byte{msb, lsb}, checksumTable)
	return msb, lsb, crc
}

func softReset(ctx context.Context, port coreio.Port) error {
	_, err := port.Write([]byte{0xD3, 0x04})
	return err
}

func stopContinuousMeasurement(ctx context.Context, port coreio.Port) error {
	_, err := port.Write([]byte{0x01, 0x04})
	return err
}

func triggerContinuousMeasurement(ctx context.Context, port coreio.Port, pressure units.Pressure) error {
	mBarMSB, mBarLSB, mBarCRC := numberToWord(pressure.Hectopascals())
	_, err := port.Write([]byte{0x00, 0x10, mBarMSB, mBarLSB, mBarCRC})
	return err
}

func setMeasurementInterval(ctx context.Context, port coreio.Port, interval time.Duration) error {
	secondsMSB, secondsLSB, secondsCRC := numberToWord(interval.Seconds())
	_, err := port.Write([]byte{0x46, 0x00, secondsMSB, secondsLSB, secondsCRC})
	return err
}

func getDataReadyStatus(ctx context.Context, port coreio.Port) (bool, error) {
	_, err := port.Write([]byte{0x02, 0x02})
	if err != nil {
		return false, err
	}

	data, err := readWords(port, 1)
	if err != nil {
		return false, errors.Wrap(err, "failed to read data ready status")
	}

	ready := data[0] == 1
	return ready, nil
}

type readings struct {
	CO2              units.Concentration
	Temperature      units.Temperature
	RelativeHumidity units.RelativeHumidity
}

func readMeasurement(ctx context.Context, port coreio.Port, pressure units.Pressure) (*readings, error) {
	_, err := port.Write([]byte{0x03, 0x00})
	if err != nil {
		return nil, err
	}

	data, err := readWords(port, 6)
	if err != nil {
		return nil, errors.Wrap(err, "failed to read measurements")
	}

	co2 := math.Float32frombits((uint32(data[0]) << 16) | uint32(data[1]))
	temp := math.Float32frombits((uint32(data[2]) << 16) | uint32(data[3]))
	rh := math.Float32frombits((uint32(data[4])<<16)|uint32(data[5])) / 100

	temperature := units.Temperature(temp * float32(units.DegreeCelsius))
	reading := &readings{
		CO2:         units.Concentration(co2 * float32(units.PartPerMillion)),
		Temperature: temperature,
		RelativeHumidity: units.RelativeHumidity{
			Temperature: temperature,
			Percentage:  float64(rh),
		},
	}
	return reading, nil
}

func readWords(port coreio.Port, words int) ([]uint16, error) {
	const (
		wordLength = 2
		crcLength  = 1
	)

	buf := make([]byte, words*(wordLength+crcLength))
	_, err := port.Read(buf)
	if err != nil {
		return nil, err
	}

	data := []uint16{}
	for idx := 0; idx < len(buf); idx += wordLength + crcLength {
		wordBytes := buf[idx : idx+2]
		expectedCrc := buf[idx+2]
		actualCrc := crc8.Checksum(wordBytes, checksumTable)
		if actualCrc != expectedCrc {
			return nil, errors.Errorf("failed to validate crc for %v (expected %v but got %v)", wordBytes, expectedCrc, actualCrc)
		}

		word := binary.BigEndian.Uint16(wordBytes[0:2])
		data = append(data, word)
	}
	return data, nil
}

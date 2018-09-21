package main

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
)

type JPEGHeader struct {
	APP1Segment *APP1Segment
}

func parseJPEGHeader(r io.Reader) (*JPEGHeader, error) {
	data, err := readBytes(r, 2)
	if err != nil {
		return nil, err
	}
	if bytes.Compare(data[0:2], []byte{0xff, 0xd8}) != 0 {
		return nil, fmt.Errorf("SOI not found")
	}
	app1, err := parseAPP1Segment(r)
	if err != nil {
		return nil, fmt.Errorf("Could not parse APP1: %s", err)
	}
	return &JPEGHeader{app1}, nil
}

type APP1Segment struct {
	Endian              binary.ByteOrder
	rawPreIFD           []byte
	IFD0                *IFD
	ExifIFD             *IFD
	GPSIFD              *IFD
	InteroperabilityIFD *IFD
	IFD1                *IFD
}

var app1marker = []byte{0xff, 0xe1}
var exifMarker = []byte{0x45, 0x78, 0x69, 0x66, 0x00, 0x00}

func parseAPP1Segment(r io.Reader) (*APP1Segment, error) {
	data, err := readBytes(r, 10)
	if err != nil {
		return nil, err
	}
	if bytes.Compare(data[0:2], app1marker) != 0 {
		return nil, fmt.Errorf("APP1 marker not found")
	}
	if bytes.Compare(data[4:10], exifMarker) != 0 {
		return nil, fmt.Errorf("Exif marker not found")
	}
	app1Length := binary.BigEndian.Uint16(data[2:4])
	tiffLength := app1Length - 10

	data, err = readBytes(r, int(tiffLength))
	if err != nil {
		return nil, err
	}
	app1, err := parseTIFF(data)
	if err != nil {
		return nil, fmt.Errorf("Could not parse APP1 segment: %s", err)
	}
	return app1, err
}

func parseTIFF(data []byte) (*APP1Segment, error) {
	var s APP1Segment
	switch {
	case bytes.Compare(data[0:2], []byte{0x4d, 0x4d}) == 0:
		s.Endian = binary.BigEndian
	case bytes.Compare(data[0:2], []byte{0x49, 0x49}) == 0:
		s.Endian = binary.LittleEndian
	default:
		return nil, fmt.Errorf("Invalid endian: %x", data[0:2])
	}
	if s.Endian.Uint16(data[2:4]) != 0x002a {
		return nil, fmt.Errorf("Invalid TIFF version: %x", data[2:4])
	}
	ifdOffset := s.Endian.Uint32(data[4:8])
	s.rawPreIFD = data[8:ifdOffset]

	var err error
	s.IFD0, err = parseIFD(data[ifdOffset:], s.Endian)
	if err != nil {
		return nil, fmt.Errorf("Could not parse 0th IFD: %s", err)
	}
	s.ExifIFD, err = s.IFD0.FindLinkedIFD(0x8769, data, s.Endian)
	if err != nil {
		return nil, fmt.Errorf("Could not parse Exif IFD: %s", err)
	}
	s.GPSIFD, err = s.IFD0.FindLinkedIFD(0x8825, data, s.Endian)
	if err != nil {
		return nil, fmt.Errorf("Could not parse GPS IFD: %s", err)
	}
	s.InteroperabilityIFD, err = s.IFD0.FindLinkedIFD(0xA005, data, s.Endian)
	if err != nil {
		return nil, fmt.Errorf("Could not parse Interoperability IFD: %s", err)
	}
	s.IFD1, err = parseIFD(data[int(ifdOffset)+len(s.IFD0.rawValues):], s.Endian)
	if err != nil {
		return nil, fmt.Errorf("Could not parse 1st IFD: %s", err)
	}
	return &s, nil
}

type IFD struct {
	Elements  []*IFDElement
	rawValues []byte
}

func (d *IFD) FindLinkedIFD(tag uint16, data []byte, endian binary.ByteOrder) (*IFD, error) {
	for _, element := range d.Elements {
		if element.Tag == tag {
			return parseIFD(data[element.ValueOffset:], endian)
		}
	}
	return nil, nil
}

func parseIFD(data []byte, endian binary.ByteOrder) (*IFD, error) {
	elementCount := endian.Uint16(data[0:2])
	valuesOffset := int(2 + elementCount*12 + 4)
	valuesLength := int(endian.Uint32(data[2+elementCount*12 : 2+elementCount*12+4]))
	ifd := &IFD{
		Elements:  make([]*IFDElement, elementCount),
		rawValues: data[valuesOffset : valuesOffset+valuesLength],
	}
	for i := 0; i < int(elementCount); i++ {
		offset := 2 + i*12
		var err error
		ifd.Elements[i], err = parseIFDElement(data[offset:offset+12], endian)
		if err != nil {
			return nil, fmt.Errorf("Could not parse IFD element #%d at 0x%x", i, offset)
		}
	}
	return ifd, nil
}

type IFDElement struct {
	Tag         uint16
	Type        IFDElementType
	Count       uint32
	ValueOffset uint32
	rawValue    []byte
}

type IFDElementType uint16

func parseIFDElement(data []byte, endian binary.ByteOrder) (*IFDElement, error) {
	if len(data) != 12 {
		return nil, fmt.Errorf("IFDElement expects 12 bytes but got %d bytes", len(data))
	}
	return &IFDElement{
		Tag:         endian.Uint16(data[0:2]),
		Type:        IFDElementType(endian.Uint16(data[2:4])),
		Count:       endian.Uint32(data[4:8]),
		ValueOffset: endian.Uint32(data[8:12]),
		rawValue:    data[8:12],
	}, nil
}

func parse(r io.Reader) (*JPEGHeader, error) {
	h, err := parseJPEGHeader(r)
	if err != nil {
		return nil, fmt.Errorf("Could not parse JPEG header: %s", err)
	}
	return h, nil
}

func readBytes(r io.Reader, length int) ([]byte, error) {
	data := make([]byte, length)
	log.Printf("Reading %d bytes", len(data))
	if n, err := r.Read(data); err != nil {
		return nil, fmt.Errorf("Could not read %d bytes: %s", length, err)
	} else if n != len(data) {
		return nil, fmt.Errorf("Expect %d bytes but got %d bytes", len(data), n)
	}
	log.Printf("%d bytes:\n%s", len(data), hex.Dump(data))
	return data, nil
}

func main() {
	filename := os.Args[1]
	r, err := os.Open(filename)
	if err != nil {
		log.Fatalf("Could not open file: %s", err)
	}
	defer r.Close()

	header, err := parse(r)
	if err != nil {
		log.Fatalf("Error: %s", err)
	}
	e := json.NewEncoder(os.Stdout)
	e.SetIndent("", " ")
	if err := e.Encode(header); err != nil {
		log.Fatalf("Could not encode to json: %s", err)
	}
}

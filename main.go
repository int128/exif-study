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
	APP1 *APP1
}

var soiMarker = []byte{0xff, 0xd8}

func parseJPEGHeader(r io.Reader) (*JPEGHeader, error) {
	b, err := readBytes(r, 2)
	if err != nil {
		return nil, err
	}
	if bytes.Compare(b, soiMarker) != 0 {
		return nil, fmt.Errorf("SOI not found")
	}
	app1, err := parseAPP1(r)
	if err != nil {
		return nil, fmt.Errorf("Could not parse APP1: %s", err)
	}
	return &JPEGHeader{app1}, nil
}

func writeJPEGHeader(w io.Writer) error {
	if err := writeBytes(w, soiMarker); err != nil {
		return err
	}
	//TODO
	return nil
}

type APP1 struct {
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

func parseAPP1(r io.Reader) (*APP1, error) {
	b, err := readBytes(r, 2)
	if err != nil {
		return nil, err
	}
	if bytes.Compare(b, app1marker) != 0 {
		return nil, fmt.Errorf("APP1 marker not found")
	}

	b, err = readBytes(r, 2)
	if err != nil {
		return nil, err
	}
	app1Length := binary.BigEndian.Uint16(b)
	tiffLength := app1Length - 10

	b, err = readBytes(r, 6)
	if err != nil {
		return nil, err
	}
	if bytes.Compare(b, exifMarker) != 0 {
		return nil, fmt.Errorf("Exif marker not found")
	}

	b, err = readBytes(r, int(tiffLength))
	if err != nil {
		return nil, err
	}
	app1, err := parseTIFF(b)
	if err != nil {
		return nil, fmt.Errorf("Could not parse TIFF: %s", err)
	}
	return app1, err
}

func parseTIFF(b []byte) (*APP1, error) {
	var app1 APP1
	switch {
	case bytes.Compare(b[0:2], []byte{0x4d, 0x4d}) == 0:
		app1.Endian = binary.BigEndian
	case bytes.Compare(b[0:2], []byte{0x49, 0x49}) == 0:
		app1.Endian = binary.LittleEndian
	default:
		return nil, fmt.Errorf("Invalid endian: %x", b[0:2])
	}
	if app1.Endian.Uint16(b[2:4]) != 0x002a {
		return nil, fmt.Errorf("Invalid TIFF version: %x", b[2:4])
	}
	ifdOffset := app1.Endian.Uint32(b[4:8])
	app1.rawPreIFD = b[8:ifdOffset]

	var err error
	app1.IFD0, err = parseIFD(b[ifdOffset:], app1.Endian)
	if err != nil {
		return nil, fmt.Errorf("Could not parse 0th IFD: %s", err)
	}
	app1.ExifIFD, err = app1.IFD0.FindLinkedIFD(0x8769, b, app1.Endian)
	if err != nil {
		return nil, fmt.Errorf("Could not parse Exif IFD: %s", err)
	}
	app1.GPSIFD, err = app1.IFD0.FindLinkedIFD(0x8825, b, app1.Endian)
	if err != nil {
		return nil, fmt.Errorf("Could not parse GPS IFD: %s", err)
	}
	app1.InteroperabilityIFD, err = app1.IFD0.FindLinkedIFD(0xA005, b, app1.Endian)
	if err != nil {
		return nil, fmt.Errorf("Could not parse Interoperability IFD: %s", err)
	}
	app1.IFD1, err = parseIFD(b[int(ifdOffset)+len(app1.IFD0.rawValues):], app1.Endian)
	if err != nil {
		return nil, fmt.Errorf("Could not parse 1st IFD: %s", err)
	}
	return &app1, nil
}

type IFD struct {
	Elements  []*IFDElement
	rawValues []byte
}

func (d *IFD) FindLinkedIFD(tag uint16, b []byte, endian binary.ByteOrder) (*IFD, error) {
	for _, e := range d.Elements {
		if e.Tag == tag {
			offset := e.Uint32(endian)
			return parseIFD(b[offset:], endian)
		}
	}
	return nil, nil
}

func parseIFD(b []byte, endian binary.ByteOrder) (*IFD, error) {
	elementCount := endian.Uint16(b[0:2])
	valuesOffset := int(2 + elementCount*12 + 4)
	valuesLength := int(endian.Uint32(b[2+elementCount*12 : 2+elementCount*12+4]))
	ifd := &IFD{
		Elements:  make([]*IFDElement, elementCount),
		rawValues: b[valuesOffset : valuesOffset+valuesLength],
	}
	for i := 0; i < int(elementCount); i++ {
		offset := 2 + i*12
		var err error
		ifd.Elements[i], err = parseIFDElement(b[offset:offset+12], b, endian)
		if err != nil {
			return nil, fmt.Errorf("Could not parse IFD element #%d at 0x%x", i, offset)
		}
	}
	return ifd, nil
}

type IFDElementType uint16

type IFDElement struct {
	Tag      uint16
	Type     IFDElementType
	Count    uint32
	Value    []byte
	rawValue []byte
}

func (e *IFDElement) Length() int {
	switch e.Type {
	case 3:
		return int(e.Count) * 2
	case 4:
		return int(e.Count) * 4
	case 5:
		return int(e.Count) * 8
	case 9:
		return int(e.Count) * 8
	case 10:
		return int(e.Count) * 16
	}
	return int(e.Count)
}

func (e *IFDElement) Uint32(endian binary.ByteOrder) uint32 {
	return endian.Uint32(e.rawValue)
}

func parseIFDElement(b []byte, ifd []byte, endian binary.ByteOrder) (*IFDElement, error) {
	if len(b) != 12 {
		return nil, fmt.Errorf("IFDElement expects 12 bytes but got %d bytes", len(b))
	}
	e := &IFDElement{
		Tag:      endian.Uint16(b[0:2]),
		Type:     IFDElementType(endian.Uint16(b[2:4])),
		Count:    endian.Uint32(b[4:8]),
		rawValue: b[8:12],
	}
	if e.Length() > 4 {
		offset := e.Uint32(endian)
		e.Value = ifd[offset : offset+uint32(e.Length())]
	} else {
		e.Value = e.rawValue
	}
	return e, nil
}

func parse(r io.Reader) (*JPEGHeader, error) {
	h, err := parseJPEGHeader(r)
	if err != nil {
		return nil, fmt.Errorf("Could not parse JPEG header: %s", err)
	}
	return h, nil
}

func readBytes(r io.Reader, length int) ([]byte, error) {
	b := make([]byte, length)
	log.Printf("Reading %d bytes", len(b))
	if n, err := r.Read(b); err != nil {
		return nil, fmt.Errorf("Could not read %d bytes: %s", len(b), err)
	} else if n != len(b) {
		return nil, fmt.Errorf("Could not read %d bytes: got %d bytes", len(b), n)
	}
	log.Printf("%d bytes:\n%s", len(b), hex.Dump(b))
	return b, nil
}

func writeBytes(w io.Writer, b []byte) error {
	log.Printf("Writing %d bytes", len(b))
	if n, err := w.Write(b); err != nil {
		return fmt.Errorf("Could not write %d bytes: %s", len(b), err)
	} else if n != len(b) {
		return fmt.Errorf("Could not write %d bytes: written %d bytes", len(b), n)
	}
	return nil
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

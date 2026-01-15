package measurement

// Unit defines the interface that all units must implement.
type Unit interface {
	ToEMU() int64
}

// Distance represents a measurement of distance with units
// that can be converted to other units.
type Distance float64

// Unit constants
const (
	// Pixel72 represents a pixel at 72 DPI
	Pixel72 Distance = 1
	// EMU represents an English Metric Unit
	EMU Distance = 1
	// Twips represents a twip (1/20 of a point)
	Twips Distance = 1
	// Point represents a point (1/72 of an inch)
	Point Distance = 1
	// Centimeter represents a centimeter
	Centimeter Distance = 10 // 1厘米 = 10毫米
	// Millimeter represents a millimeter
	Millimeter Distance = 1
)

// ToEMU converts a distance to EMUs (English Metric Units).
// 1 inch = 914400 EMUs
// 1 cm = 360000 EMUs
// 1 mm = 36000 EMUs
func ToEMU(d float64) int64 {
	// Convert millimeters to EMUs
	// 1 mm = 36000 EMUs
	return int64(d * 36000)
}

// FromEMU converts EMUs to millimeters
func FromEMU(emu int64) float64 {
	return float64(emu) / 36000
}

// Points returns the distance in points (1/72 inch)
func (d Distance) Points() float64 {
	return float64(d) * 2.83465
}

// Millimeters returns the distance in millimeters
func (d Distance) Millimeters() float64 {
	return float64(d)
}

// Inches returns the distance in inches
func (d Distance) Inches() float64 {
	return float64(d) / 25.4
}

// Centimeters returns the distance in centimeters
func (d Distance) Centimeters() float64 {
	return float64(d) / 10
}

// ToEMU converts the distance to EMUs
func (d Distance) ToEMU() int64 {
	return ToEMU(float64(d))
}

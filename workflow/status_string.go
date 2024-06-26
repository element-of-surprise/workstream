// Code generated by "stringer -type=Status"; DO NOT EDIT.

package workflow

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[NotStarted-0]
	_ = x[Running-100]
	_ = x[Completed-200]
	_ = x[Failed-300]
	_ = x[Stopped-400]
}

const (
	_Status_name_0 = "NotStarted"
	_Status_name_1 = "Running"
	_Status_name_2 = "Completed"
	_Status_name_3 = "Failed"
	_Status_name_4 = "Stopped"
)

func (i Status) String() string {
	switch {
	case i == 0:
		return _Status_name_0
	case i == 100:
		return _Status_name_1
	case i == 200:
		return _Status_name_2
	case i == 300:
		return _Status_name_3
	case i == 400:
		return _Status_name_4
	default:
		return "Status(" + strconv.FormatInt(int64(i), 10) + ")"
	}
}

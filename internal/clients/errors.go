package clients

import "errors"

var ErrOTPInvalid = errors.New("otp token is invalid or expired")

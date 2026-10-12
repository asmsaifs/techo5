//go:build !dot

package doorbell

import (
	"time"

	"github.com/HuskerMinion/techo5/echod/internal/feature/home"
)

// showFor is how long the door's camera stays up after a press.
const showFor = time.Minute

// showCamera puts the door up on the screen.
func showCamera(entity string) { home.Get().ShowCamera(entity, showFor) }

package app

import (
	"fmt"
	"io"
)

const Name = "Moon Bridge"

func Run(output io.Writer) {
	fmt.Fprintln(output, WelcomeMessage())
}

func WelcomeMessage() string {
	return "Welcome to " + Name + "!"
}

package service

import "fmt"

func GetHelloMessage(name string) string {
	return fmt.Sprintf("Hello, %s!", name)
}

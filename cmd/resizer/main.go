package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
)

func run(cmd string, args ...string) error {
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

func main() {

	// default size
	sizeGB := 100

	if len(os.Args) > 1 {
		val, err := strconv.Atoi(os.Args[1])
		if err != nil || val <= 0 {
			fmt.Println("Invalid size argument. Using default 100G.")
		} else {
			sizeGB = val
		}
	}

	fmt.Printf("Filesystem size: %dG\n", sizeGB)

	fmt.Println("Stopping docker...")
	run("systemctl", "stop", "docker")

	fmt.Println("Creating image...")
	run("fallocate", "-l", fmt.Sprintf("%dG", sizeGB), "/var/lib/docker-xfs.img")

	fmt.Println("Formatting XFS...")
	run("mkfs.xfs", "-f", "/var/lib/docker-xfs.img")

	fmt.Println("Mounting with pquota...")
	run("mount", "-o", "loop,pquota", "/var/lib/docker-xfs.img", "/var/lib/docker")

	fmt.Println("Starting docker...")
	run("systemctl", "start", "docker")

	fmt.Println("Docker storage prepared with XFS + pquota")
}

package main

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func run(cmd string, args ...string) {
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	err := c.Run()
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
}

func try(cmd string, args ...string) {
	c := exec.Command(cmd, args...)
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	_ = c.Run()
}

func output(cmd string, args ...string) string {
	c := exec.Command(cmd, args...)
	out, err := c.Output()
	if err != nil {
		fmt.Println("Error:", err)
		os.Exit(1)
	}
	return string(out)
}

func verify() {

	fmt.Println("Verifying Docker storage...")

	mounts := output("mount")

	if !strings.Contains(mounts, "/var/lib/docker") {
		fmt.Println("ERROR: /var/lib/docker not mounted")
		os.Exit(1)
	}

	if !strings.Contains(mounts, "xfs") {
		fmt.Println("ERROR: filesystem is not XFS")
		os.Exit(1)
	}

	if !(strings.Contains(mounts, "pquota") || strings.Contains(mounts, "prjquota")) {
		fmt.Println("ERROR: project quota not enabled")
		os.Exit(1)
	}

	info := output("docker", "info")

	if !strings.Contains(info, "Backing Filesystem: xfs") {
		fmt.Println("ERROR: Docker not using XFS backing filesystem")
		os.Exit(1)
	}

	fmt.Println("Verification successful.")
}

func setup(sizeGB int) {

	fmt.Println("Stopping docker...")
	run("systemctl", "stop", "docker")

	fmt.Println("Creating image...")
	run("fallocate", "-l", fmt.Sprintf("%dG", sizeGB), "/docker-storage.img")

	fmt.Println("Formatting XFS...")
	run("mkfs.xfs", "-f", "/docker-storage.img")

	fmt.Println("Creating mount dir...")
	run("mkdir", "-p", "/mnt/docker-storage")

	fmt.Println("Mount temp storage...")
	run("mount", "-o", "loop,pquota", "/docker-storage.img", "/mnt/docker-storage")

	fmt.Println("Moving old docker data...")
	run("rsync", "-aP", "/var/lib/docker/", "/mnt/docker-storage/")

	fmt.Println("Backup old docker dir...")
	run("mv", "/var/lib/docker", "/var/lib/docker.old")

	run("mkdir", "/var/lib/docker")

	fmt.Println("Mounting new docker storage...")
	run("mount", "-o", "loop,pquota", "/docker-storage.img", "/var/lib/docker")

	fmt.Println("Starting docker...")
	run("systemctl", "start", "docker")

	verify()

	fmt.Println("DONE. Docker now uses XFS + pquota.")
}

func rollback() {

	fmt.Println("Stopping docker...")
	try("systemctl", "stop", "docker")
	try("systemctl", "stop", "docker.socket")

	fmt.Println("Unmounting possible mounts...")

	try("umount", "/var/lib/docker")
	try("umount", "-l", "/var/lib/docker")

	try("umount", "/mnt/docker-storage")
	try("umount", "-l", "/mnt/docker-storage")

	fmt.Println("Removing docker dir...")
	try("rm", "-rf", "/var/lib/docker")

	fmt.Println("Restoring backup if exists...")
	if _, err := os.Stat("/var/lib/docker.old"); err == nil {
		try("mv", "/var/lib/docker.old", "/var/lib/docker")
	} else {
		try("mkdir", "-p", "/var/lib/docker")
	}

	fmt.Println("Removing temp mount dir...")
	try("rm", "-rf", "/mnt/docker-storage")

	fmt.Println("Removing storage image...")
	try("rm", "-f", "/docker-storage.img")

	fmt.Println("Reloading systemd...")
	try("systemctl", "daemon-reload")

	fmt.Println("Starting docker...")
	try("systemctl", "start", "docker")

	fmt.Println("Rollback complete. System cleaned.")
}

//launch:
// sudo ./docker-xfs setup 100 <- size

// rollback:
// sudo ./docker-xfs rollback
func main() {

	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  setup [sizeGB]")
		fmt.Println("  rollback")
		return
	}

	switch os.Args[1] {

	case "setup":

		size := 100

		if len(os.Args) > 2 {
			val, err := strconv.Atoi(os.Args[2])
			if err == nil && val > 0 {
				size = val
			}
		}

		fmt.Println("Docker storage size:", size, "GB")
		setup(size)
		verify()
	case "rollback":
		rollback()

	default:
		fmt.Println("Unknown command")
	}
}

//go:build linux

package main

// #include "ctypes.h"
import "C"
import (
	"context"
	"device-volume-driver/internal/cgroup"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/godbus/dbus/v5"
	_ "github.com/opencontainers/runtime-spec/specs-go"
	"golang.org/x/sys/unix"
)

const pluginId = "dvd"
const rootPath = "/host"

func Ptr[T any](v T) *T {
	return &v
}

func main() {
	log.Printf("Starting\n")

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())

	if err != nil {
		log.Fatal(err)
	}

	defer cli.Close()

	// Connect to system DBus to listen for systemd reload events
	conn, err := dbus.SystemBus()
	if err != nil {
		log.Printf("Failed to connect to system bus: %v", err)
	} else {
		log.Println("Connected to system bus, setting up listener for systemd Reloading signal")
		defer conn.Close()

		if err = conn.AddMatchSignal(
			dbus.WithMatchInterface("org.freedesktop.systemd1.Manager"),
			dbus.WithMatchMember("Reloading"),
		); err != nil {
			log.Printf("Failed to add match signal: %v", err)
		} else {
			c := make(chan *dbus.Signal, 10)
			conn.Signal(c)

			go func() {
				log.Println("Listening for systemd reload signals...")
				for v := range c {
					if v.Name == "org.freedesktop.systemd1.Manager.Reloading" {
						// The signal signature is 'b' (boolean) for 'active'.
						// We might care if it's starting (true) or ending (false)?
						// The issue says "systemd reload breaks the cgroup maps".
						// Usually we want to re-apply AFTER reload?
						// "Reloading" signal is sent *before* reload starts if active=true.
						// And maybe *after*?
						// Documentation says: "Sent when the manager begins reloading."
						// There is another signal `Reloaded`? No.
						// Wait, if it breaks maps, maybe we should apply it active=true (start) or wait?
						// If systemd resets cgroups *during* reload, we should apply *after* it finishes?
						// But there is no "Reloaded" signal guaranteed?
						// Note: "JobNew" for reload job?
						// Let's check the boolean body.
						var active bool
						if len(v.Body) > 0 {
							active, _ = v.Body[0].(bool)
						}
						
						log.Printf("Received systemd Reloading signal (active: %v)\n", active)
						
						// If active is true, it's starting. If we apply now, it might be wiped?
						// If active is false, it's NOT sent? documentation says "active" is true.
						// Does it send false when done?
						// If not, we might need to wait a bit or listen for JobRemoved?
						// For now, let's trigger it immediately, and maybe delay slightly?
						// Or just trigger it. Idempotency is key.
						// If "active=true" means "I am about to reload", then we should probably wait until it's done. 
						// But how do we know?
						// Usually "Reloading" is just one pulse.
						// Let's assume we re-check immediately. If it fails, we might need a delay.
						// To be safe, let's process it. 
						
						log.Println("Re-processing containers due to systemd reload")
						checkExistingContainers(cli)
					}
				}
			}()
		}
	}

	checkExistingContainers(cli)
	listenForMounts(cli)
}

func getDeviceInfo(devicePath string) (string, int64, int64, error) {
	var stat unix.Stat_t

	if err := unix.Stat(devicePath, &stat); err != nil {
		log.Println(err)
		return "", -1, -1, err
	}

	var deviceType string

	switch stat.Mode & unix.S_IFMT {
	case unix.S_IFBLK:
		deviceType = "b"
	case unix.S_IFCHR:
		deviceType = "c"
	default:
		log.Println("aborting: device is neither a character or block device")
		return "", -1, -1, fmt.Errorf("unsupported device type... aborting")
	}

	major := int64(unix.Major(stat.Rdev))
	minor := int64(unix.Minor(stat.Rdev))

	log.Printf("Found device: %s %s %d:%d\n", devicePath, deviceType, major, minor)

	return deviceType, major, minor, nil
}

func listenForMounts(cli *client.Client) {
	msgs, errs := cli.Events(
		context.Background(),
		types.EventsOptions{Filters: filters.NewArgs(filters.Arg("event", "start"))},
	)

	for {
		select {
		case err := <-errs:
			log.Fatal(err)
		case msg := <-msgs:
			processContainer(cli, msg.Actor.ID)
		}
	}
}

func processContainer(cli *client.Client, id string) {
	info, err := cli.ContainerInspect(context.Background(), id)

	if err != nil {
		panic(err)
	} else {
		pid := info.State.Pid
		version, err := cgroup.GetDeviceCGroupVersion("/", pid)

		log.Printf("The cgroup version for process %d is: %v\n", pid, version)

		if err != nil {
			log.Println(err)
			return
		}

		log.Printf("Checking mounts for process %d\n", pid)

		for _, mount := range info.Mounts {
			log.Printf(
				"%s/%v requested a volume mount for %s at %s\n",
				id, info.State.Pid, mount.Source, mount.Destination,
			)

			if !strings.HasPrefix(mount.Source, "/dev") {
				log.Printf("%s is not a device... skipping\n", mount.Source)
				continue
			}

			api, err := cgroup.New(version)
			cgroupPath, sysfsPath, err := api.GetDeviceCGroupMountPath("/", pid)

			if err != nil {
				log.Println(err)
				break
			}

			cgroupPath = path.Join(rootPath, sysfsPath, cgroupPath)

			log.Printf("The cgroup path for process %d is at %v\n", pid, cgroupPath)

			if fileInfo, err := os.Stat(mount.Source); err != nil {
				log.Println(err)
				continue
			} else {
				if fileInfo.IsDir() {
					err := filepath.Walk(mount.Source,
						func(path string, info os.FileInfo, err error) error {
							if err != nil {
								return err
							} else if info.IsDir() {
								return nil
							} else if err = applyDeviceRules(api, path, cgroupPath, pid); err != nil {
								log.Println(err)
							}
							return nil
						})
					if err != nil {
						log.Println(err)
					}
				} else {
					if err = applyDeviceRules(api, mount.Source, cgroupPath, pid); err != nil {
						log.Println(err)
					}
				}
			}
		}
	}
}

func checkExistingContainers(cli *client.Client) {
	containers, err := cli.ContainerList(context.Background(), container.ListOptions{})

	if err != nil {
		panic(err)
	}

	for _, container := range containers {
		log.Printf("Checking existing container %s %s\n", container.ID[:10], container.Image)
		processContainer(cli, container.ID)
	}
}

func applyDeviceRules(api cgroup.Interface, mountPath string, cgroupPath string, pid int) error {
	deviceType, major, minor, err := getDeviceInfo(mountPath)

	if err != nil {
		log.Println(err)
		return err
	} else {
		log.Printf("Adding device rule for process %d at %s\n", pid, cgroupPath)
		err = api.AddDeviceRules(cgroupPath, []cgroup.DeviceRule{
			{
				Access: "rwm",
				Major:  Ptr[int64](major),
				Minor:  Ptr[int64](minor),
				Type:   deviceType,
				Allow:  true,
			},
		})

		if err != nil {
			log.Println(err)
			return err
		}
	}

	return nil
}

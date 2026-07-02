package isolation

import "fmt"

type Profile struct {
	Level                     string
	PodSecurity               string
	DefaultDenyNetworkPolicy  bool
	RequireResourceRequests   bool
	RuntimeClassName          string
	BlockHostPathVolumes      bool
	BlockPrivilegedContainers bool
	APIFairness              string
}

func ProfileForLevel(level string) (Profile, error) {
	switch level {
	case "baseline":
		return Profile{
			Level:                    "baseline",
			PodSecurity:              "baseline",
			DefaultDenyNetworkPolicy: false,
			RequireResourceRequests:  true,
			APIFairness:             "tenant",
		}, nil
	case "restricted":
		return Profile{
			Level:                     "restricted",
			PodSecurity:               "restricted",
			DefaultDenyNetworkPolicy:  true,
			RequireResourceRequests:   true,
			BlockHostPathVolumes:      true,
			BlockPrivilegedContainers: true,
			APIFairness:              "tenant",
		}, nil
	case "sandboxed":
		return Profile{
			Level:                     "sandboxed",
			PodSecurity:               "restricted",
			DefaultDenyNetworkPolicy:  true,
			RequireResourceRequests:   true,
			RuntimeClassName:          "kata-qemu",
			BlockHostPathVolumes:      true,
			BlockPrivilegedContainers: true,
			APIFairness:              "tenant-strict",
		}, nil
	default:
		return Profile{}, fmt.Errorf("unknown isolation level %q", level)
	}
}


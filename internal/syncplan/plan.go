package syncplan

import (
	"fmt"
	"strings"
	"unicode"
)

const maxDNSLabelLength = 63

type ResourceRef struct {
	TenantCluster    string
	TenantNamespace  string
	VirtualNamespace string
	Kind             string
	Name             string
}

type HostTarget struct {
	Namespace string
	Name      string
}

type Plan struct {
	Input  ResourceRef
	Host   HostTarget
	Reason string
}

func Explain(ref ResourceRef) (Plan, error) {
	if ref.TenantCluster == "" {
		return Plan{}, fmt.Errorf("tenant cluster is required")
	}
	if ref.TenantNamespace == "" {
		return Plan{}, fmt.Errorf("tenant namespace is required")
	}
	if ref.VirtualNamespace == "" {
		return Plan{}, fmt.Errorf("virtual namespace is required")
	}
	if ref.Kind == "" {
		return Plan{}, fmt.Errorf("kind is required")
	}
	if ref.Name == "" {
		return Plan{}, fmt.Errorf("name is required")
	}

	hostName := HostName(ref.Name, ref.VirtualNamespace, ref.TenantCluster)
	// Only the object name is hash-bounded. The host namespace is not synthesized:
	// it is the tenant's real host namespace, which must already exist and is
	// therefore already a valid DNS-1123 label (<=63 chars) by Kubernetes API
	// validation. Hashing it would point objects at a namespace that does not
	// exist, so it is sanitized but never truncated.
	return Plan{
		Input: ref,
		Host: HostTarget{
			Namespace: SanitizeName(ref.TenantNamespace),
			Name:      hostName,
		},
		Reason: "tenantplane uses a stable name made from resource, virtual namespace, and tenant cluster; labels preserve the reverse mapping.",
	}, nil
}

// HostName builds the deterministic host object name. For inputs within the
// 63-char DNS-1123 limit the mapping is injective; longer inputs are truncated
// and suffixed with an FNV-32 hash, so the mapping is no longer guaranteed
// injective — two inputs sharing a truncated prefix could collide (a
// birthday-bounded event). Collisions are not prevented here but are detected at
// apply time by the syncer, which refuses to overwrite a host object belonging
// to a different tenant source. See internal/syncer.applyHost.
func HostName(resourceName, virtualNamespace, tenantCluster string) string {
	base := fmt.Sprintf("%s-x-%s-x-%s", resourceName, virtualNamespace, tenantCluster)
	safe := SanitizeName(base)
	if len(safe) <= maxDNSLabelLength {
		return safe
	}

	hash := fnv32(safe)
	prefixLimit := maxDNSLabelLength - len(hash) - 1
	return strings.TrimRight(safe[:prefixLimit], "-") + "-" + hash
}

func SanitizeName(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		valid := unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-'
		if !valid {
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
			continue
		}
		if r == '-' {
			if lastDash {
				continue
			}
			lastDash = true
		} else {
			lastDash = false
		}
		b.WriteRune(r)
	}

	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "x"
	}
	return out
}

func fnv32(value string) string {
	const offset32 = 2166136261
	const prime32 = 16777619
	var hash uint32 = offset32
	for i := 0; i < len(value); i++ {
		hash ^= uint32(value[i])
		hash *= prime32
	}
	return fmt.Sprintf("%08x", hash)
}


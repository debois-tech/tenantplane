package v1alpha1

import (
	runtime "k8s.io/apimachinery/pkg/runtime"
)

// Hand-written, not controller-gen output: adding controller-tools to go.mod
// (via `go get`) pulled in a transitive k8s.io/api v0.36.0 upgrade across the
// whole module, far beyond the pinned v0.29.3 this project targets. These
// four methods follow the exact shape controller-gen produces for every other
// type in zz_generated.deepcopy.go (see TenantCluster/TenantClusterStatus for
// the closest analogues) and should be replaced by real generated output if
// that dependency gap is ever resolved.

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *SyncDecision) DeepCopyInto(out *SyncDecision) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ObjectMeta.DeepCopyInto(&out.ObjectMeta)
	in.Status.DeepCopyInto(&out.Status)
}

// DeepCopy copies the receiver, creating a new SyncDecision.
func (in *SyncDecision) DeepCopy() *SyncDecision {
	if in == nil {
		return nil
	}
	out := new(SyncDecision)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject copies the receiver, creating a new runtime.Object.
func (in *SyncDecision) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *SyncDecisionList) DeepCopyInto(out *SyncDecisionList) {
	*out = *in
	out.TypeMeta = in.TypeMeta
	in.ListMeta.DeepCopyInto(&out.ListMeta)
	if in.Items != nil {
		in, out := &in.Items, &out.Items
		*out = make([]SyncDecision, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy copies the receiver, creating a new SyncDecisionList.
func (in *SyncDecisionList) DeepCopy() *SyncDecisionList {
	if in == nil {
		return nil
	}
	out := new(SyncDecisionList)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyObject copies the receiver, creating a new runtime.Object.
func (in *SyncDecisionList) DeepCopyObject() runtime.Object {
	if c := in.DeepCopy(); c != nil {
		return c
	}
	return nil
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *SyncDecisionStatus) DeepCopyInto(out *SyncDecisionStatus) {
	*out = *in
	if in.Entries != nil {
		in, out := &in.Entries, &out.Entries
		*out = make([]SyncDecisionEntry, len(*in))
		for i := range *in {
			(*in)[i].DeepCopyInto(&(*out)[i])
		}
	}
}

// DeepCopy copies the receiver, creating a new SyncDecisionStatus.
func (in *SyncDecisionStatus) DeepCopy() *SyncDecisionStatus {
	if in == nil {
		return nil
	}
	out := new(SyncDecisionStatus)
	in.DeepCopyInto(out)
	return out
}

// DeepCopyInto copies the receiver, writing into out. in must be non-nil.
func (in *SyncDecisionEntry) DeepCopyInto(out *SyncDecisionEntry) {
	*out = *in
	in.Time.DeepCopyInto(&out.Time)
}

// DeepCopy copies the receiver, creating a new SyncDecisionEntry.
func (in *SyncDecisionEntry) DeepCopy() *SyncDecisionEntry {
	if in == nil {
		return nil
	}
	out := new(SyncDecisionEntry)
	in.DeepCopyInto(out)
	return out
}

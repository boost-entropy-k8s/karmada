package validation

import (
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	metav1validation "k8s.io/apimachinery/pkg/apis/meta/v1/validation"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/apimachinery/pkg/util/validation/field"

	policyv1alpha1 "github.com/karmada-io/karmada/pkg/apis/policy/v1alpha1"
	"github.com/karmada-io/karmada/pkg/util"
)

// LabelValueMaxLength is a label's max length
const LabelValueMaxLength int = 63

// ValidatePropagationSpec validates a PropagationSpec before creation or update.
func ValidatePropagationSpec(spec policyv1alpha1.PropagationSpec) field.ErrorList {
	var allErrs field.ErrorList
	allErrs = append(allErrs, ValidatePlacement(spec.Placement, field.NewPath("spec").Child("placement"))...)
	return allErrs
}

// ValidatePlacement validates a PropagationSpec before creation or update.
func ValidatePlacement(placement policyv1alpha1.Placement, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	if placement.ClusterAffinity != nil && placement.ClusterAffinities != nil {
		allErrs = append(allErrs, field.Invalid(fldPath, placement, "clusterAffinities can not co-exist with clusterAffinity"))
	}

	allErrs = append(allErrs, ValidateClusterAffinity(placement.ClusterAffinity, fldPath.Child("clusterAffinity"))...)
	allErrs = append(allErrs, ValidateClusterAffinities(placement.ClusterAffinities, fldPath.Child("clusterAffinities"))...)
	allErrs = append(allErrs, ValidateSpreadConstraint(placement.SpreadConstraints, fldPath.Child("spreadConstraints"))...)
	return allErrs
}

// ValidateClusterAffinity validates a PropagationSpec before creation or update.
func ValidateClusterAffinity(affinity *policyv1alpha1.ClusterAffinity, fldPath *field.Path) field.ErrorList {
	if affinity == nil {
		return nil
	}

	var allErrs field.ErrorList
	err := ValidatePolicyFieldSelector(affinity.FieldSelector)
	if err != nil {
		allErrs = append(allErrs, field.Invalid(fldPath.Child("fieldSelector"), affinity.FieldSelector, err.Error()))
	}
	return allErrs
}

// ValidateClusterAffinities validates a PropagationSpec before creation or update.
func ValidateClusterAffinities(affinities []policyv1alpha1.ClusterAffinityTerm, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	affinityNames := make(map[string]bool)
	for index, term := range affinities {
		if _, exist := affinityNames[term.AffinityName]; exist {
			allErrs = append(allErrs, field.Invalid(fldPath, affinities, "each affinity term in a policy must have a unique name"))
		} else {
			affinityNames[term.AffinityName] = true
		}

		allErrs = append(allErrs, ValidateClusterAffinity(&term.ClusterAffinity, fldPath.Index(index))...)
	}
	return allErrs
}

// ValidatePolicyFieldSelector tests if the fieldSelector of propagation policy is valid.
func ValidatePolicyFieldSelector(fieldSelector *policyv1alpha1.FieldSelector) error {
	if fieldSelector == nil {
		return nil
	}

	for _, matchExpression := range fieldSelector.MatchExpressions {
		switch matchExpression.Key {
		case util.ProviderField, util.RegionField, util.ZoneField:
		default:
			return fmt.Errorf("unsupported key %q, must be provider, region, or zone", matchExpression.Key)
		}

		switch matchExpression.Operator {
		case corev1.NodeSelectorOpIn, corev1.NodeSelectorOpNotIn:
		default:
			return fmt.Errorf("unsupported operator %q, must be In or NotIn", matchExpression.Operator)
		}
	}

	return nil
}

// ValidateSpreadConstraint tests if the constraints is valid.
func ValidateSpreadConstraint(spreadConstraints []policyv1alpha1.SpreadConstraint, fldPath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	spreadByFields := sets.New[string]()
	for index, constraint := range spreadConstraints {
		// SpreadByField and SpreadByLabel should not co-exist
		if len(constraint.SpreadByField) > 0 && len(constraint.SpreadByLabel) > 0 {
			allErrs = append(allErrs, field.Invalid(fldPath.Index(index), constraint, "spreadByLabel should not co-exist with spreadByField"))
		}

		// If MaxGroups provided, it should greater or equal than MinGroups.
		if constraint.MaxGroups > 0 && constraint.MaxGroups < constraint.MinGroups {
			allErrs = append(allErrs, field.Invalid(fldPath.Index(index), constraint, "maxGroups lower than minGroups is not allowed"))
		}

		if len(constraint.SpreadByField) > 0 {
			spreadByFields.Insert(string(constraint.SpreadByField))
		}
	}

	if spreadByFields.Len() > 0 {
		// If one of spread constraints are using 'SpreadByField', the 'SpreadByFieldCluster' must be included.
		// For example, when using 'SpreadByFieldRegion' to specify region groups, at the meantime, you must use
		// 'SpreadByFieldCluster' to specify how many clusters should be selected.
		if !spreadByFields.Has(string(policyv1alpha1.SpreadByFieldCluster)) {
			allErrs = append(allErrs, field.Invalid(fldPath, spreadConstraints, "the cluster spread constraint must be enabled in one of the constraints in case of SpreadByField is enabled"))
		}
	}

	return allErrs
}

// ValidateOverrideSpec validates that the overrider specification is correctly defined.
func ValidateOverrideSpec(overrideSpec *policyv1alpha1.OverrideSpec) error {
	if overrideSpec == nil {
		return nil
	}

	if overrideSpec.OverrideRules == nil {
		return nil
	}

	//nolint:staticcheck
	// disable `deprecation` check for backward compatibility.
	if overrideSpec.TargetCluster != nil || !EmptyOverrides(overrideSpec.Overriders) {
		return fmt.Errorf("overrideRules and (overriders or targetCluster) can't co-exist")
	}

	for overrideRuleIndex, rule := range overrideSpec.OverrideRules {
		rulePath := field.NewPath("spec").Child("overrideRules").Index(overrideRuleIndex)

		// validates provided annotations.
		for annotationIndex, annotation := range rule.Overriders.AnnotationsOverrider {
			annotationPath := rulePath.Child("overriders").Child("annotationsOverrider").Index(annotationIndex)
			if err := apivalidation.ValidateAnnotations(annotation.Value, annotationPath.Child("value")).ToAggregate(); err != nil {
				return err
			}
		}

		// validates provided labels.
		for labelIndex, label := range rule.Overriders.LabelsOverrider {
			labelPath := rulePath.Child("overriders").Child("labelsOverrider").Index(labelIndex)
			if err := metav1validation.ValidateLabels(label.Value, labelPath.Child("value")).ToAggregate(); err != nil {
				return err
			}
		}
	}
	return nil
}

// EmptyOverrides check if the overriders of override policy is empty
func EmptyOverrides(overriders policyv1alpha1.Overriders) bool {
	if len(overriders.Plaintext) != 0 {
		return false
	}
	if len(overriders.ImageOverrider) != 0 {
		return false
	}
	if len(overriders.CommandOverrider) != 0 {
		return false
	}
	if len(overriders.ArgsOverrider) != 0 {
		return false
	}
	if len(overriders.LabelsOverrider) != 0 {
		return false
	}
	if len(overriders.AnnotationsOverrider) != 0 {
		return false
	}
	return true
}

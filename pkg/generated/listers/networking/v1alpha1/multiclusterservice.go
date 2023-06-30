// Code generated by lister-gen. DO NOT EDIT.

package v1alpha1

import (
	v1alpha1 "github.com/karmada-io/karmada/pkg/apis/networking/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
)

// MultiClusterServiceLister helps list MultiClusterServices.
// All objects returned here must be treated as read-only.
type MultiClusterServiceLister interface {
	// List lists all MultiClusterServices in the indexer.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.MultiClusterService, err error)
	// MultiClusterServices returns an object that can list and get MultiClusterServices.
	MultiClusterServices(namespace string) MultiClusterServiceNamespaceLister
	MultiClusterServiceListerExpansion
}

// multiClusterServiceLister implements the MultiClusterServiceLister interface.
type multiClusterServiceLister struct {
	indexer cache.Indexer
}

// NewMultiClusterServiceLister returns a new MultiClusterServiceLister.
func NewMultiClusterServiceLister(indexer cache.Indexer) MultiClusterServiceLister {
	return &multiClusterServiceLister{indexer: indexer}
}

// List lists all MultiClusterServices in the indexer.
func (s *multiClusterServiceLister) List(selector labels.Selector) (ret []*v1alpha1.MultiClusterService, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.MultiClusterService))
	})
	return ret, err
}

// MultiClusterServices returns an object that can list and get MultiClusterServices.
func (s *multiClusterServiceLister) MultiClusterServices(namespace string) MultiClusterServiceNamespaceLister {
	return multiClusterServiceNamespaceLister{indexer: s.indexer, namespace: namespace}
}

// MultiClusterServiceNamespaceLister helps list and get MultiClusterServices.
// All objects returned here must be treated as read-only.
type MultiClusterServiceNamespaceLister interface {
	// List lists all MultiClusterServices in the indexer for a given namespace.
	// Objects returned here must be treated as read-only.
	List(selector labels.Selector) (ret []*v1alpha1.MultiClusterService, err error)
	// Get retrieves the MultiClusterService from the indexer for a given namespace and name.
	// Objects returned here must be treated as read-only.
	Get(name string) (*v1alpha1.MultiClusterService, error)
	MultiClusterServiceNamespaceListerExpansion
}

// multiClusterServiceNamespaceLister implements the MultiClusterServiceNamespaceLister
// interface.
type multiClusterServiceNamespaceLister struct {
	indexer   cache.Indexer
	namespace string
}

// List lists all MultiClusterServices in the indexer for a given namespace.
func (s multiClusterServiceNamespaceLister) List(selector labels.Selector) (ret []*v1alpha1.MultiClusterService, err error) {
	err = cache.ListAllByNamespace(s.indexer, s.namespace, selector, func(m interface{}) {
		ret = append(ret, m.(*v1alpha1.MultiClusterService))
	})
	return ret, err
}

// Get retrieves the MultiClusterService from the indexer for a given namespace and name.
func (s multiClusterServiceNamespaceLister) Get(name string) (*v1alpha1.MultiClusterService, error) {
	obj, exists, err := s.indexer.GetByKey(s.namespace + "/" + name)
	if err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.NewNotFound(v1alpha1.Resource("multiclusterservice"), name)
	}
	return obj.(*v1alpha1.MultiClusterService), nil
}
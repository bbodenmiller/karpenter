/*
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package securitygroup_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "knative.dev/pkg/logging/testing"

	"github.com/aws/karpenter/pkg/apis"
	"github.com/aws/karpenter/pkg/apis/settings"
	"github.com/aws/karpenter/pkg/apis/v1alpha1"
	"github.com/aws/karpenter/pkg/test"

	coresettings "github.com/aws/karpenter-core/pkg/apis/settings"
	corev1alpha5 "github.com/aws/karpenter-core/pkg/apis/v1alpha5"
	"github.com/aws/karpenter-core/pkg/operator/injection"
	"github.com/aws/karpenter-core/pkg/operator/options"
	"github.com/aws/karpenter-core/pkg/operator/scheme"
	coretest "github.com/aws/karpenter-core/pkg/test"
	. "github.com/aws/karpenter-core/pkg/test/expectations"
)

var ctx context.Context
var stop context.CancelFunc
var opts options.Options
var env *coretest.Environment
var awsEnv *test.Environment
var provisioner *corev1alpha5.Provisioner
var nodeTemplate *v1alpha1.AWSNodeTemplate

func TestAWS(t *testing.T) {
	ctx = TestContextWithLogger(t)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provider/AWS")
}

var _ = BeforeSuite(func() {
	env = coretest.NewEnvironment(scheme.Scheme, coretest.WithCRDs(apis.CRDs...))
	ctx = coresettings.ToContext(ctx, coretest.Settings())
	ctx = settings.ToContext(ctx, test.Settings())
	ctx, stop = context.WithCancel(ctx)
	awsEnv = test.NewEnvironment(ctx, env)
})

var _ = AfterSuite(func() {
	stop()
	Expect(env.Stop()).To(Succeed(), "Failed to stop environment")
})

var _ = BeforeEach(func() {
	ctx = injection.WithOptions(ctx, opts)
	ctx = coresettings.ToContext(ctx, coretest.Settings())
	ctx = settings.ToContext(ctx, test.Settings())
	nodeTemplate = &v1alpha1.AWSNodeTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: coretest.RandomName(),
		},
		Spec: v1alpha1.AWSNodeTemplateSpec{
			AWS: v1alpha1.AWS{
				AMIFamily:             aws.String(v1alpha1.AMIFamilyAL2),
				SubnetSelector:        map[string]string{"*": "*"},
				SecurityGroupSelector: map[string]string{"*": "*"},
			},
		},
	}
	nodeTemplate.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   v1alpha1.SchemeGroupVersion.Group,
		Version: v1alpha1.SchemeGroupVersion.Version,
		Kind:    "AWSNodeTemplate",
	})
	provisioner = test.Provisioner(coretest.ProvisionerOptions{
		Requirements: []v1.NodeSelectorRequirement{{
			Key:      v1alpha1.LabelInstanceCategory,
			Operator: v1.NodeSelectorOpExists,
		}},
		ProviderRef: &corev1alpha5.MachineTemplateRef{
			APIVersion: nodeTemplate.APIVersion,
			Kind:       nodeTemplate.Kind,
			Name:       nodeTemplate.Name,
		},
	})

	awsEnv.Reset()
})

var _ = AfterEach(func() {
	ExpectCleanedUp(ctx, env.Client)
})

var _ = Describe("Security Group Provider", func() {
	It("should default to the clusters security groups", func() {
		ExpectApplied(ctx, env.Client, provisioner, nodeTemplate)
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeTemplate)
		Expect(err).To(BeNil())
		Expect(securityGroups).To(Equal([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test1"),
				GroupName: aws.String("securityGroup-test1"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("test-security-group-1"),
				}, {
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				}},
			},
			{
				GroupId:   aws.String("sg-test2"),
				GroupName: aws.String("securityGroup-test2"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("test-security-group-2"),
				}, {
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				}},
			},
			{
				GroupId:   aws.String("sg-test3"),
				GroupName: aws.String("securityGroup-test3"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("test-security-group-3"),
				}, {
					Key: aws.String("TestTag"),
				}, {
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				}},
			},
		}))
	})
	It("should discover security groups by tag", func() {
		awsEnv.EC2API.DescribeSecurityGroupsOutput.Set(&ec2.DescribeSecurityGroupsOutput{SecurityGroups: []*ec2.SecurityGroup{
			{GroupName: aws.String("test-sgName-1"), GroupId: aws.String("test-sg-1"), Tags: []*ec2.Tag{{Key: aws.String("kubernetes.io/cluster/test-cluster"), Value: aws.String("test-sg-1")}}},
			{GroupName: aws.String("test-sgName-2"), GroupId: aws.String("test-sg-2"), Tags: []*ec2.Tag{{Key: aws.String("kubernetes.io/cluster/test-cluster"), Value: aws.String("test-sg-2")}}},
		}})
		ExpectApplied(ctx, env.Client, provisioner, nodeTemplate)
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeTemplate)
		Expect(err).To(BeNil())
		Expect(securityGroups).To(Equal([]*ec2.SecurityGroup{
			{
				GroupName: aws.String("test-sgName-1"),
				GroupId:   aws.String("test-sg-1"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("kubernetes.io/cluster/test-cluster"),
					Value: aws.String("test-sg-1"),
				}},
			},
			{
				GroupName: aws.String("test-sgName-2"),
				GroupId:   aws.String("test-sg-2"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("kubernetes.io/cluster/test-cluster"),
					Value: aws.String("test-sg-2"),
				}},
			},
		}))
	})
	It("should discover security groups by multiple tag values", func() {
		nodeTemplate.Spec.SecurityGroupSelector = map[string]string{"Name": "test-security-group-1,test-security-group-2"}
		ExpectApplied(ctx, env.Client, provisioner, nodeTemplate)
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeTemplate)
		Expect(err).To(BeNil())
		Expect(securityGroups).To(Equal([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test1"),
				GroupName: aws.String("securityGroup-test1"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("test-security-group-1"),
				}, {
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				}},
			},
			{
				GroupId:   aws.String("sg-test2"),
				GroupName: aws.String("securityGroup-test2"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("test-security-group-2"),
				}, {
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				}},
			},
		}))
	})
	It("should discover security groups by ID", func() {
		nodeTemplate.Spec.SecurityGroupSelector = map[string]string{"aws-ids": "sg-test1"}
		ExpectApplied(ctx, env.Client, provisioner, nodeTemplate)
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeTemplate)
		Expect(err).To(BeNil())
		Expect(securityGroups).To(Equal([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test1"),
				GroupName: aws.String("securityGroup-test1"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("test-security-group-1"),
				}, {
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				}},
			},
		}))
	})
	It("should discover security groups by IDs", func() {
		nodeTemplate.Spec.SecurityGroupSelector = map[string]string{"aws-ids": "sg-test1,sg-test2"}
		ExpectApplied(ctx, env.Client, provisioner, nodeTemplate)
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeTemplate)
		Expect(err).To(BeNil())
		Expect(securityGroups).To(Equal([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test1"),
				GroupName: aws.String("securityGroup-test1"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("test-security-group-1"),
				}, {
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				}},
			},
			{
				GroupId:   aws.String("sg-test2"),
				GroupName: aws.String("securityGroup-test2"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("test-security-group-2"),
				}, {
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				}},
			},
		}))
	})
	It("should discover security groups by IDs and tags", func() {
		nodeTemplate.Spec.SecurityGroupSelector = map[string]string{"aws-ids": "sg-test1,sg-test2", "foo": "bar"}
		ExpectApplied(ctx, env.Client, provisioner, nodeTemplate)
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeTemplate)
		Expect(err).To(BeNil())
		Expect(securityGroups).To(Equal([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test1"),
				GroupName: aws.String("securityGroup-test1"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("test-security-group-1"),
				}, {
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				}},
			},
			{
				GroupId:   aws.String("sg-test2"),
				GroupName: aws.String("securityGroup-test2"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("test-security-group-2"),
				}, {
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				}},
			},
		}))
	})
	It("should discover security groups by IDs intersected with tags", func() {
		nodeTemplate.Spec.SecurityGroupSelector = map[string]string{"aws-ids": "sg-test2", "foo": "bar"}
		ExpectApplied(ctx, env.Client, provisioner, nodeTemplate)
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeTemplate)
		Expect(err).To(BeNil())
		Expect(securityGroups).To(Equal([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test2"),
				GroupName: aws.String("securityGroup-test2"),
				Tags: []*ec2.Tag{{
					Key:   aws.String("Name"),
					Value: aws.String("test-security-group-2"),
				}, {
					Key:   aws.String("foo"),
					Value: aws.String("bar"),
				}},
			},
		}))
	})
})

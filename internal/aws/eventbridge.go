package aws

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/eventbridge"
	eventbridgetypes "github.com/aws/aws-sdk-go-v2/service/eventbridge/types"
	"github.com/aws/aws-sdk-go-v2/service/scheduler"
	schedulertypes "github.com/aws/aws-sdk-go-v2/service/scheduler/types"
)

// EventBridgeResourceDiscovery handles discovery of EventBridge resources
type EventBridgeResourceDiscovery struct {
	client *Client
}

// NewEventBridgeResourceDiscovery creates a new EventBridge resource discovery instance
func NewEventBridgeResourceDiscovery(client *Client) *EventBridgeResourceDiscovery {
	return &EventBridgeResourceDiscovery{
		client: client,
	}
}

// DiscoverEventBusResources discovers EventBridge event buses
func (ebrd *EventBridgeResourceDiscovery) DiscoverEventBusResources(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// Create EventBridge client
	client := eventbridge.NewFromConfig(ebrd.client.Config)

	// List all event buses (no paginator in current SDK)
	result, err := client.ListEventBuses(ctx, &eventbridge.ListEventBusesInput{})
	if err != nil {
		return nil, fmt.Errorf("failed to list event buses: %w", err)
	}

	for _, eventBus := range result.EventBuses {
		if eventBus.Arn == nil {
			continue
		}

		resource := Resource{
			ARN:  *eventBus.Arn,
			Tags: make(map[string]string),
		}

		// Get event bus tags
		tagsResult, err := client.ListTagsForResource(ctx, &eventbridge.ListTagsForResourceInput{
			ResourceARN: eventBus.Arn,
		})
		if err == nil && tagsResult.Tags != nil {
			for _, tag := range tagsResult.Tags {
				if tag.Key != nil && tag.Value != nil {
					resource.Tags[*tag.Key] = *tag.Value
				}
			}
		}

		resources = append(resources, resource)
	}

	return resources, nil
}

// DiscoverScheduleGroupResources discovers EventBridge scheduler schedule groups
func (ebrd *EventBridgeResourceDiscovery) DiscoverScheduleGroupResources(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// Create Scheduler client
	client := scheduler.NewFromConfig(ebrd.client.Config)

	// List all schedule groups
	paginator := scheduler.NewListScheduleGroupsPaginator(client, &scheduler.ListScheduleGroupsInput{})

	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list schedule groups: %w", err)
		}

		for _, scheduleGroup := range page.ScheduleGroups {
			if scheduleGroup.Arn == nil {
				continue
			}

			resource := Resource{
				ARN:  *scheduleGroup.Arn,
				Tags: make(map[string]string),
			}

			// Get schedule group tags
			tagsResult, err := client.ListTagsForResource(ctx, &scheduler.ListTagsForResourceInput{
				ResourceArn: scheduleGroup.Arn,
			})
			if err == nil && tagsResult.Tags != nil {
				for _, tag := range tagsResult.Tags {
					if tag.Key != nil && tag.Value != nil {
						resource.Tags[*tag.Key] = *tag.Value
					}
				}
			}

			resources = append(resources, resource)
		}
	}

	return resources, nil
}

// DiscoverEventBridgeResources discovers all EventBridge resources (event buses and schedule groups)
func (ebrd *EventBridgeResourceDiscovery) DiscoverEventBridgeResources(ctx context.Context) ([]Resource, error) {
	var resources []Resource

	// Discover event buses
	eventBusResources, err := ebrd.DiscoverEventBusResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover event buses: %w", err)
	}
	resources = append(resources, eventBusResources...)

	// Discover schedule groups
	scheduleGroupResources, err := ebrd.DiscoverScheduleGroupResources(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to discover schedule groups: %w", err)
	}
	resources = append(resources, scheduleGroupResources...)

	return resources, nil
}

// TagEventBridgeResources tags EventBridge event buses and schedule groups with provided tags
func (ebrd *EventBridgeResourceDiscovery) TagEventBridgeResources(ctx context.Context, arns []string, tags map[string]string) error {
	// Create clients
	eventBridgeClient := eventbridge.NewFromConfig(ebrd.client.Config)
	schedulerClient := scheduler.NewFromConfig(ebrd.client.Config)

	// Convert tags to AWS SDK format for EventBridge
	var eventBridgeTags []eventbridgetypes.Tag
	for k, v := range tags {
		eventBridgeTags = append(eventBridgeTags, eventbridgetypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	// Convert tags to AWS SDK format for Scheduler
	var schedulerTags []schedulertypes.Tag
	for k, v := range tags {
		schedulerTags = append(schedulerTags, schedulertypes.Tag{
			Key:   aws.String(k),
			Value: aws.String(v),
		})
	}

	// Process each ARN based on its type
	for _, arn := range arns {
		if strings.Contains(arn, ":event-bus/") {
			// Tag EventBridge event bus
			_, err := eventBridgeClient.TagResource(ctx, &eventbridge.TagResourceInput{
				ResourceARN: aws.String(arn),
				Tags:        eventBridgeTags,
			})
			if err != nil {
				return fmt.Errorf("failed to tag EventBridge event bus %s: %w", arn, err)
			}
		} else if strings.Contains(arn, ":schedule-group/") {
			// Tag EventBridge Scheduler schedule group
			_, err := schedulerClient.TagResource(ctx, &scheduler.TagResourceInput{
				ResourceArn: aws.String(arn),
				Tags:        schedulerTags,
			})
			if err != nil {
				return fmt.Errorf("failed to tag EventBridge schedule group %s: %w", arn, err)
			}
		} else {
			return fmt.Errorf("unknown EventBridge resource type for ARN: %s", arn)
		}
	}

	return nil
}

// GetEventBridgeResourceType determines the type of EventBridge resource from its ARN
func (ebrd *EventBridgeResourceDiscovery) GetEventBridgeResourceType(arn string) string {
	if strings.Contains(arn, ":event-bus/") {
		return "eventbus"
	}
	if strings.Contains(arn, ":schedule-group/") {
		return "schedulegroup"
	}
	return "unknown"
}

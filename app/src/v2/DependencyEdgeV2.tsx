import { BezierEdge, useInternalNode, type EdgeProps } from "@xyflow/react";
import { GRAPH_SPACING, JOB_WIDTH, JOB_HEIGHT, JOB_TAGGED_HEIGHT } from "./dependency-layout";

/** Anchor to card borders: measured 18px handles and static 6px handles differ. */
export function DependencyEdgeV2(props: EdgeProps) {
  const source = useInternalNode(props.source);
  const target = useInternalNode(props.target);
  const sourceHeight = source?.data.manualOrder || source?.data.dryRun ? JOB_TAGGED_HEIGHT : JOB_HEIGHT;
  return <BezierEdge {...props}
    sourceX={source ? source.internals.positionAbsolute.x + JOB_WIDTH / 2 : props.sourceX}
    sourceY={(source ? source.internals.positionAbsolute.y + sourceHeight : props.sourceY) + GRAPH_SPACING.arrow}
    targetX={target ? target.internals.positionAbsolute.x + JOB_WIDTH / 2 : props.targetX}
    targetY={(target ? target.internals.positionAbsolute.y : props.targetY) - GRAPH_SPACING.arrow}
  />;
}

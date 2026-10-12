export type Role = "owner" | "editor" | "viewer";
export type Lifecycle = "已发布" | "未发布";
export type WorkflowState =
  | "待制作"
  | "已制作"
  | "待排期"
  | "已排期"
  | Lifecycle;

export interface User {
  id: number;
  team_id: number;
  email: string;
  name: string;
  role: Role;
  team_name?: string;
}

export interface ContentRecord {
  id: string;
  team_id: number;
  title: string;
  category: string;
  platform: string;
  published_at: string;
  lifecycle: Lifecycle;
  workflow_state: WorkflowState;
  views: number;
  likes: number;
  comments: number;
  saves: number;
  shares: number;
  tags: string[];
}

export interface ApiErrorBody {
  detail: string;
  error: { code: string; message: string; request_id: string };
}

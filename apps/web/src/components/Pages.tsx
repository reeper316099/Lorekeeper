import { Empty } from './ui';

export function Soon({ title, milestone }: { title: string; milestone: string }) {
  return <Empty title={title}>This screen is planned for {milestone}.</Empty>;
}

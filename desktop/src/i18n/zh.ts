import { chartZh } from './slices/chart';
import { reviewZh } from './slices/review';
import { shellZh } from './slices/shell';
import { watchZh } from './slices/watch';

export const zh = {
  ...shellZh,
  ...chartZh,
  ...watchZh,
  ...reviewZh,
} as const;

export type TranslationKey = keyof typeof zh;

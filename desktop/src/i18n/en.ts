import { chartEn } from './slices/chart';
import { reviewEn } from './slices/review';
import { shellEn } from './slices/shell';
import { watchEn } from './slices/watch';
import type { TranslationKey } from './zh';

export const en: Record<TranslationKey, string> = {
  ...shellEn,
  ...chartEn,
  ...watchEn,
  ...reviewEn,
};

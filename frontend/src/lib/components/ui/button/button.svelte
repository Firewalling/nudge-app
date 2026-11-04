<script lang="ts" module>
  import { tv, type VariantProps } from 'tailwind-variants';
  import type { HTMLButtonAttributes } from 'svelte/elements';

  export const buttonVariants = tv({
    base: 'inline-flex shrink-0 items-center justify-center whitespace-nowrap rounded-md border border-transparent text-sm font-medium outline-none transition-all focus-visible:ring-2 disabled:pointer-events-none disabled:opacity-50',
    variants: {
      variant: {
        default: 'button primary',
        secondary: 'button secondary',
      },
    },
    defaultVariants: { variant: 'default' },
  });
  export type ButtonProps = HTMLButtonAttributes & { variant?: VariantProps<typeof buttonVariants>['variant'] };
</script>

<script lang="ts">
  let { class: className, variant = 'default', type = 'button', children, ...restProps }: ButtonProps = $props();
  import { cn } from '$lib/utils';
</script>

<button data-slot="button" class={cn(buttonVariants({ variant }), className)} {type} {...restProps}>
  {@render children?.()}
</button>

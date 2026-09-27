/**
 * Ambient types for the virtual modules served by plugins in `vite.config.ts`.
 */

declare module 'virtual:provider-icons' {
  /** Basenames (without the `.png` extension) of every PNG in `public/providers`. */
  const iconIds: string[]
  export default iconIds
}

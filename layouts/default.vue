<template>
  <div class="min-h-screen flex flex-col bg-suno-bg text-suno-text-light font-sans">
    <AppNav />
    <main class="flex-1 w-full">
      <slot />
    </main>
    <AppFooter />
    <SupportWidget />
  </div>
</template>

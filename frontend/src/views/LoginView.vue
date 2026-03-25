<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-50">
    <div class="max-w-md w-full space-y-8 p-8 bg-white rounded-lg shadow">
      <div class="text-center">
        <h1 class="text-3xl font-bold text-gray-900">LLM Proxy</h1>
        <p class="mt-2 text-sm text-gray-600">Sign in to access the admin console</p>
      </div>
      <div>
        <button
          @click="signIn"
          class="w-full flex justify-center py-3 px-4 border border-transparent rounded-md shadow-sm text-sm font-medium text-white bg-indigo-600 hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-offset-2 focus:ring-indigo-500"
        >
          Sign in with PocketID
        </button>
      </div>
    </div>
  </div>
</template>

<script setup>
function signIn() {
  window.location.href = '/auth/login'
}
</script>

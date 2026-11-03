<template>
  <section class="card overflow-hidden" aria-label="Key budgets">
    <div class="p-5 border-b border-gray-100 dark:border-gray-700">
      <h2 class="font-semibold">Key budgets</h2>
      <p class="text-xs text-gray-500 dark:text-gray-400 mt-1">
        Active keys · spend in the selected time window across all models.
        Configured caps and enforcement are unchanged.
      </p>
    </div>
    <div class="overflow-x-auto">
      <table class="w-full text-sm">
        <thead>
          <tr
            class="text-left text-xs text-gray-500 dark:text-gray-400 border-b border-gray-100 dark:border-gray-700"
          >
            <th class="px-5 py-3">Key / application</th>
            <th class="px-5 py-3">Spend</th>
            <th class="px-5 py-3">Hard budget</th>
            <th class="px-5 py-3">% budget</th>
            <th class="px-5 py-3">Status</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!rows.length">
            <td
              colspan="5"
              class="p-8 text-center text-gray-500 dark:text-gray-400"
            >
              No active keys match these filters.
            </td>
          </tr>
          <tr
            v-for="row in rows"
            :key="row.key_id"
            class="border-b border-gray-50 dark:border-gray-700"
          >
            <td class="px-5 py-3">
              {{ row.key_name
              }}<span class="block text-xs text-gray-500 dark:text-gray-400"
                >{{ row.app_name }} · {{ row.team_name }}</span
              >
            </td>
            <td class="px-5 py-3 tabular-nums">
              {{ formatMoney(row.total_spend) }}
            </td>
            <td class="px-5 py-3 tabular-nums">
              {{
                row.max_budget == null ? '—' : `$${row.max_budget.toFixed(2)}`
              }}
            </td>
            <td class="px-5 py-3 tabular-nums">
              {{
                row.max_budget > 0
                  ? `${((row.total_spend / row.max_budget) * 100).toFixed(1)}%`
                  : '—'
              }}
            </td>
            <td class="px-5 py-3">
              <StatusBadge
                :status="
                  row.max_budget != null && row.total_spend >= row.max_budget
                    ? 'over'
                    : row.soft_budget != null &&
                        row.total_spend >= row.soft_budget
                      ? 'warning'
                      : 'ok'
                "
              />
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
<script setup>
import StatusBadge from './StatusBadge.vue'
import { formatMoney } from '../utils/usage.js'
defineProps({ rows: { type: Array, default: () => [] } })
</script>

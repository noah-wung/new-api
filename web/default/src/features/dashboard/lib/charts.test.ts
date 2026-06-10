import assert from 'node:assert/strict'
import { describe, test } from 'node:test'
import { processChartData } from './charts'
import type { QuotaDataItem } from '../types'

describe('processChartData token distribution', () => {
  test('builds token distribution specs from model token usage', () => {
    const data: QuotaDataItem[] = [
      {
        created_at: 1717200000,
        model_name: 'gpt-4o-mini',
        quota: 120,
        token_used: 1500,
        count: 2,
      },
      {
        created_at: 1717200000,
        model_name: 'gpt-4o-mini',
        quota: 80,
        token_used: 2500,
        count: 1,
      },
    ]

    const chartData = processChartData(data, 'hour') as ReturnType<
      typeof processChartData
    > & {
      spec_token_line: {
        data: Array<{ values: Array<Record<string, unknown>> }>
        yField: string
      }
      totalTokensDisplay: string
    }

    const values = chartData.spec_token_line.data[0]?.values ?? []

    assert.equal(chartData.totalTokensDisplay, '4.0K')
    assert.equal(chartData.spec_token_line.yField, 'rawTokens')
    const nonZeroValues = values.filter((item) => item.rawTokens !== 0)
    assert.equal(nonZeroValues.length, 1)
    assert.equal(nonZeroValues[0]?.Model, 'gpt-4o-mini')
    assert.equal(nonZeroValues[0]?.rawTokens, 4000)
    assert.equal(nonZeroValues[0]?.TimeSum, 4000)
  })
})

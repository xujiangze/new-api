/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { MAX_CHART_TREND_POINTS } from '@/features/dashboard/constants'
import type {
  ProcessedCacheHitRateChartData,
  QuotaDataItem,
} from '@/features/dashboard/types'
import { formatNumber } from '@/lib/format'
import { formatChartTime, type TimeGranularity } from '@/lib/time'

import { getDashboardChartColors } from './charts'
import { computeCacheHitRatePercent, getRowCacheHitMetrics } from './stats'

type TFunction = (key: string) => string

interface BucketCacheAggregate {
  count: number
  inputTokens: number
  hitReadTokens: number
  cacheReadTokens: number
  cacheCreationTokens: number
}

interface TrendDatum {
  Time: string
  Series: string
  HitRate: number | null
  Count: number
  InputTokens: number
  HitReadTokens: number
  CacheWriteTokens: number
}

interface ComparisonDatum {
  Model: string
  HitRate: number
  Count: number
  InputTokens: number
  HitReadTokens: number
  CacheWriteTokens: number
}

interface TooltipLineItem {
  key: string
  value: string | number
  datum?: Record<string, unknown>
}

const MAX_TREND_SERIES_MODELS = 10
const MAX_COMPARISON_MODELS = 20

function createEmptyAggregate(): BucketCacheAggregate {
  return {
    count: 0,
    inputTokens: 0,
    hitReadTokens: 0,
    cacheReadTokens: 0,
    cacheCreationTokens: 0,
  }
}

function addAggregate(
  target: BucketCacheAggregate,
  count: number,
  inputTokens: number,
  hitReadTokens: number,
  cacheReadTokens: number,
  cacheCreationTokens: number
): BucketCacheAggregate {
  return {
    count: target.count + count,
    inputTokens: target.inputTokens + inputTokens,
    hitReadTokens: target.hitReadTokens + hitReadTokens,
    cacheReadTokens: target.cacheReadTokens + cacheReadTokens,
    cacheCreationTokens: target.cacheCreationTokens + cacheCreationTokens,
  }
}

export function formatDashboardHitRate(
  rate: number,
  locale?: Intl.LocalesArgument
): string {
  const clamped = Math.min(100, Math.max(0, Number.isFinite(rate) ? rate : 0))
  return Intl.NumberFormat(locale, {
    style: 'percent',
    maximumFractionDigits: 1,
  }).format(clamped / 100)
}

function roundHitRate(rate: number): number {
  return Number(Math.min(100, Math.max(0, rate)).toFixed(1))
}

export function processCacheHitRateChartData(
  data: QuotaDataItem[],
  timeGranularity: TimeGranularity = 'day',
  t?: TFunction,
  locale?: Intl.LocalesArgument,
  chartCornerRadius?: number
): ProcessedCacheHitRateChartData {
  const tt: TFunction = t ?? ((x) => x)
  const overallLabel = tt('Overall')
  const formatRate = (rate: number) => formatDashboardHitRate(rate, locale)
  const formatTokenCount = (value: number) => formatNumber(value, locale)

  if (!data || data.length === 0) {
    return {
      spec_hit_rate_trend: {
        type: 'line',
        data: [{ id: 'hitRateTrendData', values: [] }],
        xField: 'Time',
        yField: 'HitRate',
        seriesField: 'Series',
        invalidType: 'break',
        axes: [
          { orient: 'bottom', type: 'band' },
          {
            orient: 'left',
            type: 'linear',
            min: 0,
            max: 100,
            label: {
              formatMethod: (val: string | number) => `${val}%`,
            },
          },
        ],
        legends: { visible: false },
        title: {
          visible: true,
          text: tt('Hit Rate Trend'),
          subtext: tt('No data available'),
        },
        background: { fill: 'transparent' },
      },
      spec_hit_rate_comparison: {
        type: 'bar',
        data: [{ id: 'hitRateComparisonData', values: [] }],
        direction: 'horizontal',
        xField: 'HitRate',
        yField: 'Model',
        seriesField: 'Model',
        axes: [
          {
            orient: 'bottom',
            type: 'linear',
            min: 0,
            max: 100,
            label: {
              formatMethod: (val: string | number) => `${val}%`,
            },
          },
          { orient: 'left', type: 'band' },
        ],
        legends: { visible: false },
        title: {
          visible: true,
          text: tt('Model Comparison'),
          subtext: tt('No data available'),
        },
        background: { fill: 'transparent' },
      },
      overallHitRate: 0,
      overallHitRateDisplay: formatRate(0),
      totalInputTokens: 0,
      totalHitReadTokens: 0,
      totalCacheCreationTokens: 0,
    }
  }

  const timeModelMap = new Map<string, Map<string, BucketCacheAggregate>>()
  const timeOverallMap = new Map<string, BucketCacheAggregate>()
  const modelTotalsMap = new Map<string, BucketCacheAggregate>()
  const timeTimestamps = new Map<string, number>()

  let overallTotals = createEmptyAggregate()

  for (const item of data) {
    const timestamp = Number(item.created_at)
    const timeKey = formatChartTime(timestamp, timeGranularity)
    timeTimestamps.set(
      timeKey,
      Math.min(timeTimestamps.get(timeKey) ?? timestamp, timestamp)
    )
    const model = item.model_name || 'Unknown'
    const count = Math.max(0, Number(item.count) || 0)
    const cache = getRowCacheHitMetrics(item)

    let modelMap = timeModelMap.get(timeKey)
    if (!modelMap) {
      modelMap = new Map()
      timeModelMap.set(timeKey, modelMap)
    }
    modelMap.set(
      model,
      addAggregate(
        modelMap.get(model) ?? createEmptyAggregate(),
        count,
        cache.inputTokens,
        cache.hitReadTokens,
        cache.cacheReadTokens,
        cache.cacheCreationTokens
      )
    )

    timeOverallMap.set(
      timeKey,
      addAggregate(
        timeOverallMap.get(timeKey) ?? createEmptyAggregate(),
        count,
        cache.inputTokens,
        cache.hitReadTokens,
        cache.cacheReadTokens,
        cache.cacheCreationTokens
      )
    )

    modelTotalsMap.set(
      model,
      addAggregate(
        modelTotalsMap.get(model) ?? createEmptyAggregate(),
        count,
        cache.inputTokens,
        cache.hitReadTokens,
        cache.cacheReadTokens,
        cache.cacheCreationTokens
      )
    )

    overallTotals = addAggregate(
      overallTotals,
      count,
      cache.inputTokens,
      cache.hitReadTokens,
      cache.cacheReadTokens,
      cache.cacheCreationTokens
    )
  }

  const sortedTimes = [...timeTimestamps]
    .sort((a, b) => a[1] - b[1])
    .map(([time]) => time)

  let chartTimes = sortedTimes
  if (sortedTimes.length < MAX_CHART_TREND_POINTS) {
    const lastTime = Math.max(
      ...data.map((item) => Number(item.created_at) || 0)
    )
    let intervalSec = 3600
    if (timeGranularity === 'week') {
      intervalSec = 604800
    } else if (timeGranularity === 'day') {
      intervalSec = 86400
    }
    chartTimes = Array.from({ length: MAX_CHART_TREND_POINTS }, (_, i) =>
      formatChartTime(
        lastTime - (MAX_CHART_TREND_POINTS - 1 - i) * intervalSec,
        timeGranularity
      )
    )
  }

  const rankedModels = [...modelTotalsMap.entries()]
    .sort((a, b) => {
      if (b[1].inputTokens !== a[1].inputTokens) {
        return b[1].inputTokens - a[1].inputTokens
      }
      if (b[1].count !== a[1].count) {
        return b[1].count - a[1].count
      }
      return a[0].localeCompare(b[0])
    })
    .map(([model]) => model)

  const topTrendModels = rankedModels.slice(0, MAX_TREND_SERIES_MODELS)
  const includeOverallSeries = topTrendModels.length > 1
  const trendSeriesNames = includeOverallSeries
    ? [overallLabel, ...topTrendModels]
    : topTrendModels

  const trendValues: TrendDatum[] = []
  for (const time of chartTimes) {
    if (includeOverallSeries) {
      const bucket = timeOverallMap.get(time)
      const hasRequests = Boolean(bucket && bucket.count > 0)
      const hitRate = hasRequests
        ? roundHitRate(
            computeCacheHitRatePercent(
              bucket?.hitReadTokens ?? 0,
              bucket?.inputTokens ?? 0
            )
          )
        : null
      trendValues.push({
        Time: time,
        Series: overallLabel,
        HitRate: hitRate,
        Count: bucket?.count ?? 0,
        InputTokens: bucket?.inputTokens ?? 0,
        HitReadTokens: bucket?.hitReadTokens ?? 0,
        CacheWriteTokens: bucket?.cacheCreationTokens ?? 0,
      })
    }

    for (const model of topTrendModels) {
      const bucket = timeModelMap.get(time)?.get(model)
      const hasRequests = Boolean(bucket && bucket.count > 0)
      const hitRate = hasRequests
        ? roundHitRate(
            computeCacheHitRatePercent(
              bucket?.hitReadTokens ?? 0,
              bucket?.inputTokens ?? 0
            )
          )
        : null
      trendValues.push({
        Time: time,
        Series: model,
        HitRate: hitRate,
        Count: bucket?.count ?? 0,
        InputTokens: bucket?.inputTokens ?? 0,
        HitReadTokens: bucket?.hitReadTokens ?? 0,
        CacheWriteTokens: bucket?.cacheCreationTokens ?? 0,
      })
    }
  }

  const comparisonValues: ComparisonDatum[] = [...modelTotalsMap.entries()]
    .map(([model, agg]) => ({
      Model: model,
      HitRate: roundHitRate(
        computeCacheHitRatePercent(agg.hitReadTokens, agg.inputTokens)
      ),
      Count: agg.count,
      InputTokens: agg.inputTokens,
      HitReadTokens: agg.hitReadTokens,
      CacheWriteTokens: agg.cacheCreationTokens,
    }))
    .sort((a, b) => {
      if (b.HitRate !== a.HitRate) return b.HitRate - a.HitRate
      if (b.InputTokens !== a.InputTokens) return b.InputTokens - a.InputTokens
      if (b.Count !== a.Count) return b.Count - a.Count
      return a.Model.localeCompare(b.Model)
    })
    .slice(0, MAX_COMPARISON_MODELS)

  const sortedModels = [...modelTotalsMap.keys()].sort()
  const colorDomain = includeOverallSeries
    ? [overallLabel, ...sortedModels]
    : sortedModels
  const palette = getDashboardChartColors(colorDomain.length)
  const seriesColor = {
    type: 'ordinal',
    domain: colorDomain,
    range: palette,
  }

  const overallHitRate = computeCacheHitRatePercent(
    overallTotals.hitReadTokens,
    overallTotals.inputTokens
  )

  return {
    spec_hit_rate_trend: {
      type: 'line',
      data: [{ id: 'hitRateTrendData', values: trendValues }],
      xField: 'Time',
      yField: 'HitRate',
      seriesField: 'Series',
      invalidType: 'break',
      color: seriesColor,
      axes: [
        { orient: 'bottom', type: 'band' },
        {
          orient: 'left',
          type: 'linear',
          min: 0,
          max: 100,
          label: {
            formatMethod: (val: string | number) => `${val}%`,
          },
        },
      ],
      legends: {
        visible: trendSeriesNames.length > 0,
        selectMode: 'multiple',
      },
      line: {
        style: {
          lineWidth: (datum: Record<string, unknown>) =>
            datum?.Series === overallLabel ? 2.5 : 2,
          curveType: 'monotone',
          lineDash: (datum: Record<string, unknown>) =>
            datum?.Series === overallLabel ? [5, 3] : [0],
        },
      },
      point: {
        visible: true,
        style: {
          size: 4,
        },
      },
      tooltip: {
        mark: {
          content: [
            {
              key: (datum: Record<string, unknown>) => datum?.Series,
              value: (datum: Record<string, unknown>) =>
                datum?.HitRate == null
                  ? '-'
                  : formatRate(Number(datum.HitRate)),
            },
            {
              key: tt('Cache Read'),
              value: (datum: Record<string, unknown>) =>
                formatTokenCount(Number(datum?.HitReadTokens) || 0),
            },
            {
              key: tt('Input Tokens'),
              value: (datum: Record<string, unknown>) =>
                formatTokenCount(Number(datum?.InputTokens) || 0),
            },
            {
              key: tt('Cache Write'),
              value: (datum: Record<string, unknown>) =>
                formatTokenCount(Number(datum?.CacheWriteTokens) || 0),
            },
          ],
        },
        dimension: {
          content: [
            {
              key: (datum: Record<string, unknown>) => datum?.Series,
              value: (datum: Record<string, unknown>) => datum?.HitRate,
            },
          ],
          updateContent: (items: TooltipLineItem[]) => {
            const active = items.filter(
              (item) =>
                item.datum != null &&
                item.datum.HitRate !== null &&
                item.datum.HitRate !== undefined
            )
            const overallItems = active.filter(
              (item) => item.key === overallLabel
            )
            const modelItems = active
              .filter((item) => item.key !== overallLabel)
              .sort((a, b) => (Number(b.value) || 0) - (Number(a.value) || 0))
            return [...overallItems, ...modelItems].map((item) => {
              const hitRead = Number(item.datum?.HitReadTokens) || 0
              const input = Number(item.datum?.InputTokens) || 0
              const rateText = formatRate(Number(item.value) || 0)
              return {
                ...item,
                value:
                  input > 0
                    ? `${rateText} (${formatTokenCount(hitRead)} / ${formatTokenCount(input)})`
                    : rateText,
              }
            })
          },
        },
      },
      background: { fill: 'transparent' },
      animation: true,
    },
    spec_hit_rate_comparison: {
      type: 'bar',
      data: [{ id: 'hitRateComparisonData', values: comparisonValues }],
      direction: 'horizontal',
      xField: 'HitRate',
      yField: 'Model',
      seriesField: 'Model',
      color: seriesColor,
      axes: [
        {
          orient: 'bottom',
          type: 'linear',
          min: 0,
          max: 100,
          label: {
            formatMethod: (val: string | number) => `${val}%`,
          },
        },
        { orient: 'left', type: 'band' },
      ],
      legends: { visible: false },
      bar: {
        style:
          chartCornerRadius == null ? {} : { cornerRadius: chartCornerRadius },
        state: {
          hover: { stroke: '#000', lineWidth: 1 },
        },
      },
      label: {
        visible: true,
        position: 'right',
        formatMethod: (val: string | number) => formatRate(Number(val) || 0),
      },
      tooltip: {
        mark: {
          content: [
            {
              key: tt('Cache Hit Rate'),
              value: (datum: Record<string, unknown>) =>
                formatRate(Number(datum?.HitRate) || 0),
            },
            {
              key: tt('Cache Read'),
              value: (datum: Record<string, unknown>) =>
                formatTokenCount(Number(datum?.HitReadTokens) || 0),
            },
            {
              key: tt('Input Tokens'),
              value: (datum: Record<string, unknown>) =>
                formatTokenCount(Number(datum?.InputTokens) || 0),
            },
            {
              key: tt('Cache Write'),
              value: (datum: Record<string, unknown>) =>
                formatTokenCount(Number(datum?.CacheWriteTokens) || 0),
            },
          ],
        },
      },
      background: { fill: 'transparent' },
      animation: true,
    },
    overallHitRate,
    overallHitRateDisplay: formatRate(overallHitRate),
    totalInputTokens: overallTotals.inputTokens,
    totalHitReadTokens: overallTotals.hitReadTokens,
    totalCacheCreationTokens: overallTotals.cacheCreationTokens,
  }
}

$(function() {
  Highcharts.setOptions({
    global: {
      timezoneOffset: 8 * 60 // PST
    }
  });
})


function normData(data) {
  var QPSLENGTH = 100
  var now = Date.now()
  // Normalize to QPSLENGTH data points.
  if (!data || data.length === 0) {
    data = [[now, 0]]
  }
  while (data.length < QPSLENGTH) {
    data.unshift([data[0][0]-1000, 0])
  }
  while (data.length > QPSLENGTH) {
    data.shift()
  }
  return data
}


function NewChart(node, height) {
  var chart

  if (!height) {
    height = 168
  }

  node.qpshistory = normData(node.qpshistory)

  chart = new Highcharts.Chart({
    chart: {
      renderTo: 'chart-' + node.id,
      type: 'column',
      backgroundColor: 'transparent',
      height: height,
      marginLeft: 3,
      marginRight: 3,
      marginBottom: 0,
      marginTop: 0,
      zoomType: 'x'
    },
    title: {
      text: ''
    },
    yAxis: {
      gridLineWidth: 0
    },
    xAxis: {
      type: 'datetime',
    },
    series: [{
      name: 'QPS',
      color: '#fff',
      type: 'line',
      data: node.qpshistory
    }],
    credits: {
      enabled: false
    },
    legend: {
      enabled: false
    },
    plotOptions: {
      column: {
        borderWidth: 0,
        color: '#b2c831',
        shadow: false
      },
      line: {
        lineWidth: 1,
      },
      series: {
        marker: {
          enabled: false,
          radius: 1,
        },
        states: {
          hover: {
            halo: {
              size: 2,
            }
          }
        }
      }
    },
    tooltip: {
      backgroundColor: null,
      borderWidth: 0,
      shadow: false,
      useHTML: true,
      style: {
        padding: 0
      },
      positioner: function () {
        return { x: 150, y: -8 };
      },
      style: {
        color: 'white',
      },
      headerFormat: '<span class="pull-right" style="font-size: 10px;">{point.key}</span>',
      pointFormat: '<span class="pull-right" style="font-size: 12px;"> QPS {point.y:.0f}</span>',
    },

  })

  chart.addPoint = function(point) {
    chart.series[0].addPoint(point, true, true)
  }

  return chart
}

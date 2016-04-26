package main

import (
	"fmt"
	"math"
	"time"

	db "github.com/SoftwareDefinedBuildings/btrdb-go"
	"github.com/go-gl/mathgl/mgl32"
	"gopkg.in/qml.v1"
	"gopkg.in/qml.v1/gl/2.0"
	"gopkg.in/qml.v1/gl/glbase"
)

type BTrDBPlotter struct {
	qml.Object
	numPoints int
	//Obviously this will all change so that the plotter can do multiple
	//streams, this is just my 5AM hack
	data        []db.StatisticalValue
	gfxData     []float32
	timeStart   int64
	timeWidth   int64
	yMin        float64
	yMax        float64
	streamColor [4]float32 //RGBA
	//what time is zero in timeval space
	timeEpoch int64
	//nanoseconds per timeval time unit
	timeScale float64
	//what value is zero in timeval space
	valEpoch float64
	//val units per timeval val unit
	valScale float64

	//Performance enhancement, keep the data on the graphics card if it
	//rather than sending per frame
	buffers     []glbase.Buffer
	bufferClean bool

	shadersCompiled bool
	shaders         glbase.Program

	shader_Stats     glbase.Attrib
	shader_Top       glbase.Attrib
	shader_Matrix    glbase.Uniform
	shader_Color     glbase.Uniform
	shader_MeanWidth glbase.Uniform
}

func InitBTrDBPlotter(p *BTrDBPlotter, obj qml.Object) {
	//This field is inherited from qml.Object
	p.Object = obj

	// make dummy data for now
	// Obviously we will need a better way of choosing the scale values
	// so that we don't blow out the precision of our float32
	p.timeEpoch = int64(365 * 24 * time.Hour) //one year past 1970
	p.timeScale = float64(time.Millisecond)
	p.valEpoch = 0
	p.valScale = 1
	p.streamColor = [4]float32{1.0, 1.0, 0.0, 1.0}
	const alen = 10
	data := make([]db.StatisticalValue, alen)
	for i := 0; i < alen; i++ {
		data[i].Min = math.Sin(float64(i)/100) - 0.5
		data[i].Max = math.Sin(float64(i)/100) + 0.5
		data[i].Mean = math.Sin(float64(i)/100) * 0.7
		data[i].Time = p.timeEpoch + int64(float64(int64(i)*int64(time.Minute)))
	}
	p.timeStart = p.timeEpoch
	p.timeWidth = data[alen-1].Time - data[0].Time
	p.yMin = -2.0
	p.yMax = 2.0
	p.SetData(data)
}
func (p *BTrDBPlotter) SetData(d []db.StatisticalValue) {
	p.data = d
	numElements := len(d)
	const elPerVertex = 5
	//We have to duplicate the last element (fencepost)
	numElements += 1
	//Two unique vertices per element
	numElements *= 2
	//five attributes per element (vec4+bool)
	numElements *= elPerVertex
	p.gfxData = make([]float32, numElements)

	//Now to convert
	for idx, e := range d {
		time := float64(e.Time-p.timeEpoch) / p.timeScale
		min := (e.Min - p.valEpoch) / p.valScale
		mean := (e.Mean - p.valEpoch) / p.valScale
		max := (e.Max - p.valEpoch) / p.valScale
		p.gfxData[idx*elPerVertex*2+0] = float32(time)
		p.gfxData[idx*elPerVertex*2+1] = float32(min)
		p.gfxData[idx*elPerVertex*2+2] = float32(mean)
		p.gfxData[idx*elPerVertex*2+3] = float32(max)
		p.gfxData[idx*elPerVertex*2+4] = 0
		p.gfxData[idx*elPerVertex*2+elPerVertex+0] = float32(time)
		p.gfxData[idx*elPerVertex*2+elPerVertex+1] = float32(min)
		p.gfxData[idx*elPerVertex*2+elPerVertex+2] = float32(mean)
		p.gfxData[idx*elPerVertex*2+elPerVertex+3] = float32(max)
		p.gfxData[idx*elPerVertex*2+elPerVertex+4] = 1
	}

	//And fill in the last value
	lastIdx := numElements - elPerVertex*2
	//TODO BUG this might not be the best way of determining the point width
	timeDelta := float32(float64(d[1].Time-d[0].Time) / p.timeScale)
	p.gfxData[lastIdx+0] = p.gfxData[lastIdx+0-elPerVertex*2] + timeDelta
	p.gfxData[lastIdx+1] = p.gfxData[lastIdx+1-elPerVertex*2]
	p.gfxData[lastIdx+2] = p.gfxData[lastIdx+2-elPerVertex*2]
	p.gfxData[lastIdx+3] = p.gfxData[lastIdx+3-elPerVertex*2]
	p.gfxData[lastIdx+4] = p.gfxData[lastIdx+4-elPerVertex*2]
	p.gfxData[lastIdx+5] = p.gfxData[lastIdx+5-elPerVertex*2] + timeDelta
	p.gfxData[lastIdx+6] = p.gfxData[lastIdx+6-elPerVertex*2]
	p.gfxData[lastIdx+7] = p.gfxData[lastIdx+7-elPerVertex*2]
	p.gfxData[lastIdx+8] = p.gfxData[lastIdx+8-elPerVertex*2]
	p.gfxData[lastIdx+9] = p.gfxData[lastIdx+9-elPerVertex*2]

	//Mark as requiring copy to the graphics card
	p.bufferClean = false
}

func (p *BTrDBPlotter) checkGPUState(gl *GL.GL) {

	if !p.shadersCompiled {
		vshader := gl.CreateShader(GL.VERTEX_SHADER)
		gl.ShaderSource(vshader, vertexShader)
		gl.CompileShader(vshader)

		var status [1]int32
		gl.GetShaderiv(vshader, GL.COMPILE_STATUS, status[:])
		if status[0] == 0 {
			log := gl.GetShaderInfoLog(vshader)
			panic("vertex shader compilation failed: " + string(log))
		}

		fshader := gl.CreateShader(GL.FRAGMENT_SHADER)
		gl.ShaderSource(fshader, fragmentShader)
		gl.CompileShader(fshader)

		gl.GetShaderiv(fshader, GL.COMPILE_STATUS, status[:])
		if status[0] == 0 {
			log := gl.GetShaderInfoLog(fshader)
			panic("fragment shader compilation failed: " + string(log))
		}
		p.shaders = gl.CreateProgram()
		gl.AttachShader(p.shaders, vshader)
		gl.AttachShader(p.shaders, fshader)
		gl.LinkProgram(p.shaders)

		p.shader_Stats = gl.GetAttribLocation(p.shaders, "stats")
		p.shader_Top = gl.GetAttribLocation(p.shaders, "top")
		p.shader_Color = gl.GetUniformLocation(p.shaders, "color")
		p.shader_MeanWidth = gl.GetUniformLocation(p.shaders, "meanwidth")
		p.shader_Matrix = gl.GetUniformLocation(p.shaders, "matrix")
		p.shadersCompiled = true
	}
	if p.bufferClean {
		return
	}

	if p.buffers != nil {
		gl.DeleteBuffers(p.buffers)
		p.buffers = nil
	}
	p.buffers = gl.GenBuffers(1)
	gl.BindBuffer(GL.ARRAY_BUFFER, p.buffers[0])
	fmt.Println("gfxdata len is ", len(p.gfxData))
	gl.BufferData(GL.ARRAY_BUFFER, 0, p.gfxData, GL.STATIC_DRAW)
	p.bufferClean = true

}

//We use OpenGL GLES GLSL
var vertexShader = `
#version 120

// INPUTS
// time, min, mean, max
attribute highp vec4 stats;
attribute highp float top;
// map timeval coordinates to screen coordinates
uniform highp mat4 matrix;
// the stream color
uniform highp vec4 color;
// In screan coordinates
uniform highp float meanwidth;

// OUTPUTS
varying highp vec4 fragColor;
varying highp vec2 meanBounds;
varying highp vec3 screenSpace;
void main()
{
  //Calculate the timeval coordinate of the vertex
  vec4 timeval = vec4(stats.x, (top > 0.5 ? stats.w : stats.y), 0, 1);

  //map the timeval coordinate to screen space
  gl_Position = matrix * timeval;
  screenSpace = gl_Position.xyz;

  //forward the color
  fragColor = color;

  //forward the mean bounds TODO
  vec4 meanloc = matrix * vec4(timeval.x, stats.z,0,1);
  meanBounds = vec2(meanloc.y-meanwidth, meanloc.y+meanwidth);
}
`

var fragmentShader = `
#version 120

// INPUTS
in highp vec4 fragColor;
in highp vec2 meanBounds;
in highp vec3 screenSpace;

void main()
{
    if (screenSpace.y >= meanBounds.x && screenSpace.y <= meanBounds.y) {
      gl_FragColor = fragColor;
    } else {
      vec4 dimmed = fragColor;
      dimmed.w *= 0.6;
      gl_FragColor = dimmed;
    }
}
`

func (p *BTrDBPlotter) Paint(ptr *qml.Painter) {
	//Get handle to OpenGL ES API
	gl := GL.API(ptr)

	// check the GPU has the latest arrays and the shaders and stuff
	p.checkGPUState(gl)

	//Calculate our matrix
	startInTimeval := float32(float64(p.timeStart-p.timeEpoch) / p.timeScale)
	endInTimeval := startInTimeval + float32(float64(p.timeWidth)/p.timeScale)
	//Obviously we would have multiple matrixes, one for each stream
	minValInTimeval := float32((p.yMin - p.valEpoch) / p.valScale)
	maxValInTimeval := float32((p.yMax - p.valEpoch) / p.valScale)

	fmt.Printf("left=%v right=%v top=%v bottom=%v\n", startInTimeval, endInTimeval, maxValInTimeval, minValInTimeval)
	matrix := mgl32.Ortho2D(startInTimeval, endInTimeval,
		minValInTimeval, maxValInTimeval)

	//	matrix = matrix.Mul4(mgl32.Scale3D(0.2, 0.5, 0.5))
	matrix = mgl32.Scale3D(0.2, 0.5, 0.5).Mul4(matrix)
	vertices := make([]float32, (len(p.gfxData)/5)*2)
	idx := 0
	for i := 0; i < len(p.gfxData); i += 5 {
		fmt.Printf("vtx %d\n", i)
		fmt.Printf("  time %f\n", p.gfxData[i])
		fmt.Printf("   min %f\n", p.gfxData[i+1])
		fmt.Printf("  mean %f\n", p.gfxData[i+2])
		fmt.Printf("   max %f\n", p.gfxData[i+3])
		fmt.Printf("   top %f\n", p.gfxData[i+4])
		if p.gfxData[i+4] > 0.5 {
			//TOP
			timeval := mgl32.Vec4{p.gfxData[i], p.gfxData[i+3], 0, 1}
			screen := matrix.Mul4x1(timeval)
			vertices[idx] = screen[0]
			idx++
			vertices[idx] = screen[1]
			idx++
		} else {
			timeval := mgl32.Vec4{p.gfxData[i], p.gfxData[i+1], 0, 1}
			screen := matrix.Mul4x1(timeval)
			vertices[idx] = screen[0]
			idx++
			vertices[idx] = screen[1]
			idx++
		}
	}

	for i := 0; i < len(vertices); i += 2 {
		fmt.Printf("v %d %v,%v\n", i/2, vertices[i], vertices[i+1])
	}
	//	fmt.Println(vertices)

	gl.ClearColor(1, 0, 0, 1)
	gl.Clear(GL.COLOR_BUFFER_BIT)
	// Activate the shader
	gl.UseProgram(p.shaders)

	// Configure the uniform variables for the shaders
	gl.UniformMatrix4fv(p.shader_Matrix, false, matrix[:])
	gl.Uniform4fv(p.shader_Color, p.streamColor[:])
	gl.Uniform1f(p.shader_MeanWidth, 0.01) //not used TODO

	// Configure the attribute variables for the shaders
	gl.BindBuffer(GL.ARRAY_BUFFER, p.buffers[0])
	gl.VertexAttribPointer(p.shader_Stats, 4, GL.FLOAT, false, 5*4, 0)
	gl.EnableVertexAttribArray(p.shader_Stats)
	gl.VertexAttribPointer(p.shader_Top, 1, GL.FLOAT, false, 5*4, 4*4)
	gl.EnableVertexAttribArray(p.shader_Top)

	// Draw it all
	gl.Enable(GL.BLEND)
	gl.BlendFunc(GL.SRC_ALPHA, GL.ONE_MINUS_SRC_ALPHA)

	fmt.Println("number of thingies", len(p.gfxData)/5)
	gl.DrawArrays(GL.TRIANGLE_STRIP, 0, len(p.gfxData)/5)

}

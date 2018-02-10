package inject

import "github.com/facebookgo/inject"
import "github.com/jrivets/gorivets"
import "errors"
import "bytes"
import "strconv"
import "fmt"

// Use the Injector to provide dependency injection (DI) design pattern. The
// Injector can construct and populate Components (a structs which fields
// are tagged by `inject: "..."` directive).
//
// The following workflow is suppossed to be done:
// 1. Create an injector by injector := newInjector() call.
// 2. Register components like injector.Register(&Component{comp, "the component name"} ...)
// 3. Do the injection by injector.construct(). Be sure that the call CAN CONSTRUCT
//    components THAT WERE NOT REGISTERED, but refferred by. So, for example, if
//    a Component fields requires injection with empty name tag (`inject:""`) AND
//    there is no registered component with the name (empty), the injector will
//    create a new instance of the refferred component.
// 4. execute the command and shutdown the injector just before exit from the program
//
// The injector supports LifeCycler pattern which allows to initialize components
// that implement the interface in a specific order and de-initialize them in reverse
// of the initialization order.
type Injector struct {
	logger      gorivets.Logger
	fbLogger    gorivets.Logger
	fbInjector  *inject.Graph
	lcComps     *gorivets.SortedSlice
	constructed bool
}

type Component struct {
	Component interface{}
	Name      string
}

// A component can implement the interface when the component has a life-cycle
// All life-cyclers are ordered by DiPhase value after creation and injection.
// Components with lower value are initialized first
type LifeCycler interface {
	DiPhase() int
	DiInit() error
	DiShutdown()
}

// A component can implement the interface to be notified by the injector
// after all dependencies are injected
type PostConstructor interface {
	DiPostConstruct()
}

func (c *Component) lifeCycler() LifeCycler {
	lc, ok := c.Component.(LifeCycler)
	if ok {
		return lc
	}
	return nil
}

func (c *Component) postConstructor() PostConstructor {
	pc, ok := c.Component.(PostConstructor)
	if ok {
		return pc
	}
	return nil
}

func (c *Component) getPhase() int {
	return c.lifeCycler().DiPhase()
}

func (c *Component) String() string {
	lc := c.lifeCycler()
	var buffer bytes.Buffer
	buffer.WriteString("Component: {\"")
	buffer.WriteString("\", lifeCycler: ")
	if !gorivets.IsNil(lc) {
		buffer.WriteString("yes, phase: ")
		buffer.WriteString(strconv.FormatInt(int64(lc.DiPhase()), 10))
	} else {
		buffer.WriteString("no")
	}
	buffer.WriteString(", Component: ")
	buffer.WriteString(fmt.Sprintf("%v", c.Component))
	buffer.WriteString("}")
	return buffer.String()
}

var lfCompare = func(a, b interface{}) int {
	p1 := a.(*Component).getPhase()
	p2 := b.(*Component).getPhase()
	return gorivets.CompareInt(p1, p2)
}

func NewInjector(logger gorivets.Logger, loggerFb gorivets.Logger) *Injector {
	fbInjector := &inject.Graph{}
	injector := &Injector{logger: logger, fbLogger: loggerFb, fbInjector: fbInjector}
	fbInjector.Logger = injector
	return injector
}

func (i *Injector) Debugf(format string, v ...interface{}) {
	i.fbLogger.Debug(fmt.Sprintf(format, v...))
}

func (i *Injector) RegisterOne(ifs interface{}, name string) {
	obj := &inject.Object{Value: ifs, Name: name}
	i.fbInjector.Provide(obj)
}

func (i *Injector) RegisterMany(comps ...interface{}) {
	var objects []*Component = make([]*Component, len(comps))
	for idx, c := range comps {
		objects[idx] = &Component{Component: c}
	}
	i.Register(objects...)
}

func (i *Injector) Register(comps ...*Component) {
	var objects []*inject.Object = make([]*inject.Object, len(comps))
	for idx, c := range comps {
		objects[idx] = &inject.Object{Value: c.Component, Name: c.Name}
		i.logger.Debug("Registering ", c)
	}
	i.fbInjector.Provide(objects...)
}

func (i *Injector) Construct() {
	i.logger.Info("Initializing...")
	if i.constructed {
		panic("The dependency Inector already initialized. Injector.Construct() can be called once!")
	}
	i.constructed = true
	if err := i.fbInjector.Populate(); err != nil {
		i.logger.Error("Got the error while initialization: ", err)
		panic(err)
	}

	i.afterPopulation()

	defer func() {
		r := recover()
		if r != nil {
			i.logger.Error("Rolling back life-cyclers due to panic in initialization cycle.")
			i.shutdownLcs()
			panic(r)
		}
	}()

	if err := i.initLcs(); err != nil {
		panic(err)
	}
}

func (i *Injector) Shutdown() {
	i.logger.Info("Shutdown.")
	i.shutdownLcs()
}

func (i *Injector) newLcComps() {
	size := 10
	if i.lcComps != nil {
		size = i.lcComps.Len()
	}
	i.lcComps, _ = gorivets.NewSortedSliceByComp(lfCompare, size)
}

// it scans all FB objects and makes components from them.
// Also it builds sorted list of life cyclers and call for post constructors
func (i *Injector) afterPopulation() {
	i.newLcComps()
	cmpMap := make(map[interface{}]*Component)
	i.logger.Debug("Scanning all objects after population")
	for _, o := range i.fbInjector.Objects() {
		if gorivets.IsNil(o.Value) {
			continue
		}

		if _, ok := cmpMap[o.Value]; ok {
			continue
		}
		comp := &Component{Component: o.Value, Name: o.Name}
		cmpMap[o.Value] = comp

		pc := comp.postConstructor()
		if pc != nil {
			i.logger.Debug("Post construct: ", comp)
			pc.DiPostConstruct()
		}

		lfCycler := comp.lifeCycler()
		if lfCycler != nil {
			i.logger.Debug("Found LifeCycler ", comp)
			i.lcComps.Add(comp)
		}
	}
}

func (i *Injector) initLcs() error {
	i.logger.Info("Initializing life-cyclers (", i.lcComps.Len(), " will be initialized)")
	lcComps := i.lcComps.Copy()
	i.newLcComps()
	for _, comp := range lcComps {
		i.logger.Info("Initializing ", comp)
		c := comp.(*Component)
		lc := c.lifeCycler()
		if err := lc.DiInit(); err != nil {
			return errors.New("Error while initializing ")
		}
		i.lcComps.Add(c)
	}
	return nil
}

func (i *Injector) shutdownLcs() {
	if i.lcComps == nil {
		i.logger.Info("Life cyclers shutdowner: they were not initialized.")
		return
	}
	lcComps := i.lcComps.Copy()
	i.logger.Info("Shutting down life-cyclers (", i.lcComps.Len(), " will be shut down)")
	i.lcComps = nil
	for idx := len(lcComps) - 1; idx >= 0; idx-- {
		c := lcComps[idx].(*Component)
		i.logger.Info("Shutting down ", c)
		c.lifeCycler().DiShutdown()
	}
}

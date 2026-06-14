import{f as r,O as a,B as i,F as s}from"./three.module-NTdkD2Ht.js";const u={name:"CopyShader",uniforms:{tDiffuse:{value:null},opacity:{value:1}},vertexShader:`

		varying vec2 vUv;

		void main() {

			vUv = uv;
			gl_Position = projectionMatrix * modelViewMatrix * vec4( position, 1.0 );

		}`,fragmentShader:`

		uniform float opacity;

		uniform sampler2D tDiffuse;

		varying vec2 vUv;

		void main() {

			vec4 texel = texture2D( tDiffuse, vUv );
			gl_FragColor = opacity * texel;


		}`};class c{constructor(){this.isPass=!0,this.enabled=!0,this.needsSwap=!0,this.clear=!1,this.renderToScreen=!1}setSize(){}render(){console.error("THREE.Pass: .render() must be implemented in derived pass.")}dispose(){}}const o=new a(-1,1,1,-1,0,1);class n extends i{constructor(){super(),this.setAttribute("position",new s([-1,3,0,-1,-1,0,3,-1,0],3)),this.setAttribute("uv",new s([0,2,0,0,2,0],2))}}const l=new n;class h{constructor(e){this._mesh=new r(l,e)}dispose(){this._mesh.geometry.dispose()}render(e){e.render(this._mesh,o)}get material(){return this._mesh.material}set material(e){this._mesh.material=e}}export{u as C,h as F,c as P};

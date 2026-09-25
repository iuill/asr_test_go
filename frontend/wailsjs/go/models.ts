export namespace main {

	export class Model {
	    id: string;
	    name: string;
	    provider: string;
	    available: boolean;
	    reason?: string;

	    static createFrom(source: any = {}) {
	        return new Model(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.id = source["id"];
	        this.name = source["name"];
	        this.provider = source["provider"];
	        this.available = source["available"];
	        this.reason = source["reason"];
	    }
	}
	export class AppInfo {
	    version: string;
	    configPath: string;
	    models: Model[];
	    debugLogging: boolean;
	    showTimestamps: boolean;
	    autoSave: boolean;
	    autoSaveDir: string;
	    logPath: string;
	    logError?: string;
	    error?: string;

	    static createFrom(source: any = {}) {
	        return new AppInfo(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.version = source["version"];
	        this.configPath = source["configPath"];
	        this.models = this.convertValues(source["models"], Model);
	        this.debugLogging = source["debugLogging"];
	        this.showTimestamps = source["showTimestamps"];
	        this.autoSave = source["autoSave"];
	        this.autoSaveDir = source["autoSaveDir"];
	        this.logPath = source["logPath"];
	        this.logError = source["logError"];
	        this.error = source["error"];
	    }

		convertValues(a: any, classs: any, asMap: boolean = false): any {
		    if (!a) {
		        return a;
		    }
		    if (a.slice && a.map) {
		        return (a as any[]).map(elem => this.convertValues(elem, classs));
		    } else if ("object" === typeof a) {
		        if (asMap) {
		            for (const key of Object.keys(a)) {
		                a[key] = new classs(a[key]);
		            }
		            return a;
		        }
		        return new classs(a);
		    }
		    return a;
		}
	}

	export class Transcript {
	    text: string;

	    static createFrom(source: any = {}) {
	        return new Transcript(source);
	    }

	    constructor(source: any = {}) {
	        if ('string' === typeof source) source = JSON.parse(source);
	        this.text = source["text"];
	    }
	}

}

"""Exercise the real production setup wizard, including its provider probes."""
import re
from html.parser import HTMLParser
from client import eventually


class SavedFields(HTMLParser):
    def __init__(self, page):
        super().__init__()
        self.values={}
        self.active=False
        self.textarea=None
        self.feed(page)

    def handle_starttag(self, tag, attrs):
        attrs=dict(attrs)
        if tag=='form':self.active=attrs.get('action')=='setup/save'
        if not self.active:return
        name=attrs.get('name')
        if tag=='input' and name and attrs.get('type')!='checkbox':self.values[name]=attrs.get('value','')
        if tag=='textarea' and name:self.textarea=name;self.values[name]=''

    def handle_data(self, value):
        if self.active and self.textarea:self.values[self.textarea]+=value

    def handle_endtag(self, tag):
        if tag=='form':self.active=False
        if tag=='textarea':self.textarea=None


class Wizard:
    def __init__(self, application, restart):
        self.app=application
        self.restart=restart

    def revision(self):
        page=self.app.call('/admin/setup?step=mail',text=True)
        found=re.search(r'name="revision" value="([0-9]+)"',page)
        if not found:raise RuntimeError('Setup revision field missing')
        return found.group(1)

    def bootstrap(self, setup_token, email, password, steps):
        eventually(lambda:self.app.call('/api/v1/setup/status').get('open'),'fresh production bootstrap')
        self.app.call('/api/v1/setup/claim',{'token':setup_token,'email':email,'name':'Stack acceptance operator','password':password})
        self.app.call('/api/v1/auth/login',{'email':email,'password':password})
        for step,values in steps.items():
            saved=SavedFields(self.app.call('/admin/setup?step='+step,text=True)).values
            saved.update(values)
            self.app.call('/admin/setup/save',dict(saved,revision=self.revision(),step=step,back='?step='+step),form=True)
        for kind,confirmation in [('mail','send'),('storage','check')]:
            page=self.app.call('/admin/setup?step=review',text=True)
            found=re.search('action="setup/check-'+kind+'".*?name="check_id" value="([^"]+)"',page)
            if not found:raise RuntimeError('Setup provider-probe form missing')
            self.app.call('/admin/setup/check-'+kind,{'revision':self.revision(),'check_id':found.group(1),'confirm':confirmation,'back':'?step=review'},form=True)
        for kind in ['matrix','collabora','livekit','ai','smip']:
            page=self.app.call('/admin/setup?step=review',text=True)
            forms=re.findall(r'action="setup/check-integration"(.*?)</form>',page,re.S)
            form=next((row for row in forms if 'name="kind" value="'+kind+'"' in row),None)
            if form is None:raise RuntimeError('Setup integration-probe form missing: '+kind)
            found=re.search(r'name="check_id" value="([^"]+)"',form)
            if not found:raise RuntimeError('Setup integration-probe intent missing: '+kind)
            self.app.call('/admin/setup/check-integration',{'revision':self.revision(),'check_id':found.group(1),'kind':kind,'confirm':'check','back':'?step=review'},form=True)
        page=self.app.call('/admin/setup?step=review',text=True)
        if 'Not ready to publish:' in page:raise RuntimeError('Real provider checks did not permit publication')
        self.app.call('/admin/setup/publish',{'revision':self.revision(),'confirm':'publish','back':'?step=review'},form=True)
        self.restart()
        eventually(lambda:self.app.call('/api/v1/setup/status').get('active'),'published production setup activation')
